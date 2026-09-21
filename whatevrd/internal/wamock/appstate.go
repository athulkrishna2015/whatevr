//go:build whatevr_mock

package wamock

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waServerSync"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// mockAppState is the server half of app state: one sync key, and the patches
// that describe what the account already has. The encoding is whatsmeow's own
// Processor, running against a store that exists only in memory, so what the
// client validates is the real MAC chain rather than something shaped like one.
type mockAppState struct {
	mu sync.Mutex

	keyID   []byte
	keyData []byte
	proc    *appstate.Processor

	built   bool
	patches map[appstate.WAPatchName][][]byte
	states  map[appstate.WAPatchName]appstate.HashState

	// sharedAt is when the key went out, and ready says the client has it.
	sharedAt time.Time
	ready    bool
}

func newMockAppState(r *seededRand) *mockAppState {
	device := &store.Device{
		Log:          waLog.Noop,
		AppStateKeys: newMemAppStateKeys(),
		AppState:     newMemAppStateStore(),
	}
	state := &mockAppState{
		keyID:   r.bytes(6),
		keyData: r.bytes(32),
		proc:    appstate.NewProcessor(device, waLog.Noop),
		patches: map[appstate.WAPatchName][][]byte{},
		states:  map[appstate.WAPatchName]appstate.HashState{},
	}
	_ = device.AppStateKeys.PutAppStateSyncKey(context.Background(), state.keyID, store.AppStateSyncKey{
		Data:      state.keyData,
		Timestamp: Ago(90 * 24 * time.Hour).UnixMilli(),
	})
	return state
}

// build turns the world into patches, once. The push name is the load-bearing
// one: whatsmeow refuses to send presence without it, so without this the
// presence view never reports anybody.
func (m *mockAppState) build(ctx context.Context, w *World) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.built {
		return nil
	}
	m.built = true

	var infos []appstate.PatchInfo
	infos = append(infos, appstate.BuildSettingPushName(w.Self().Name))

	w.mu.Lock()
	chats := append([]*Chat(nil), w.order...)
	w.mu.Unlock()
	for _, chat := range chats {
		if chat.pinOrder > 0 {
			infos = append(infos, appstate.BuildPin(chat.JID, true))
		}
		if chat.archived {
			infos = append(infos, appstate.BuildArchive(chat.JID, true, time.Time{}, nil))
		}
		if chat.muted {
			infos = append(infos, appstate.BuildMuteAbs(chat.JID, true, nil))
		}
	}

	for _, info := range infos {
		if info.Timestamp.IsZero() {
			info.Timestamp = Ago(48 * time.Hour)
		}
		encoded, err := m.proc.EncodePatch(ctx, m.keyID, m.states[info.Type], info)
		if err != nil {
			return fmt.Errorf("encode %s patch: %w", info.Type, err)
		}
		if _, err := m.applyPatch(ctx, nil, info.Type, encoded); err != nil {
			return err
		}
	}
	return nil
}

// applyPatch stamps a patch with its version, then runs it through the same
// decoder the client will, which is what produces the authoritative next hash
// state. Doing it this way rather than tracking the hash by hand is deliberate:
// EncodePatch takes its state by value, so the hash it computes is lost, and a
// server that guessed at it would hand out patches nobody can verify.
//
// Neither MAC covers the version field, so stamping it after the fact is safe;
// the same is not true of anything else in here.
func (m *mockAppState) applyPatch(ctx context.Context, w *World, name appstate.WAPatchName, encoded []byte) ([]appstate.Mutation, error) {
	state := m.states[name]
	var patch waServerSync.SyncdPatch
	if err := proto.Unmarshal(encoded, &patch); err != nil {
		return nil, fmt.Errorf("reparse %s patch: %w", name, err)
	}
	patch.Version = &waServerSync.SyncdVersion{Version: proto.Uint64(state.Version + 1)}
	stamped, err := proto.Marshal(&patch)
	if err != nil {
		return nil, fmt.Errorf("remarshal %s patch: %w", name, err)
	}
	mutations, newState, err := m.proc.DecodePatches(ctx, &appstate.PatchList{
		Name:    name,
		Patches: []*waServerSync.SyncdPatch{&patch},
	}, state, true)
	if err != nil {
		return nil, fmt.Errorf("verify %s patch: %w", name, err)
	}
	m.states[name] = newState
	m.patches[name] = append(m.patches[name], stamped)
	applyMutations(w, mutations)
	return mutations, nil
}

// collection answers one collection of the sync query, from the version the
// client says it already has.
func (m *mockAppState) collection(name string, from uint64) waBinary.Node {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := m.patches[appstate.WAPatchName(name)]
	if from > uint64(len(all)) {
		from = uint64(len(all))
	}
	rest := all[from:]
	nodes := make([]waBinary.Node, 0, len(rest))
	for _, patch := range rest {
		nodes = append(nodes, waBinary.Node{Tag: "patch", Content: patch})
	}
	node := waBinary.Node{
		Tag: "collection",
		Attrs: waBinary.Attrs{
			"name":             name,
			"version":          fmt.Sprintf("%d", len(all)),
			"has_more_patches": "false",
		},
	}
	if len(nodes) > 0 {
		node.Content = []waBinary.Node{{Tag: "patches", Content: nodes}}
	}
	return node
}

// noteShared records that the key is on the wire.
func (m *mockAppState) noteShared() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sharedAt = time.Now()
}

// clientHasKey reports whether the client can decode a patch yet. whatsmeow
// re-syncs every collection the moment a key share lands, and critical_block is
// one only it asks for, so a request naming it is the proof. The timeout is the
// fallback for the day that stops being true: serving patches nobody can read
// is a confusing failure, but so is serving none forever.
func (m *mockAppState) clientHasKey(names []string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ready {
		return true
	}
	for _, name := range names {
		if appstate.WAPatchName(name) == appstate.WAPatchCriticalBlock {
			m.ready = true
			return true
		}
	}
	if !m.sharedAt.IsZero() && time.Since(m.sharedAt) > keyShareGrace {
		m.ready = true
	}
	return m.ready
}

// keyShareGrace is how long the mock waits for the client to act on the key
// before assuming it did.
const keyShareGrace = 5 * time.Second

// keyShareMessage is the protocol message that hands the client the key every
// patch is encrypted under. whatsmeow re-syncs every collection the moment it
// lands, which is what makes the ordering against the daemon's own first fetch
// not matter.
func (m *mockAppState) keyShareMessage() *waE2E.Message {
	return &waE2E.Message{
		ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_APP_STATE_SYNC_KEY_SHARE.Enum(),
			AppStateSyncKeyShare: &waE2E.AppStateSyncKeyShare{
				Keys: []*waE2E.AppStateSyncKey{{
					KeyID: &waE2E.AppStateSyncKeyId{KeyID: m.keyID},
					KeyData: &waE2E.AppStateSyncKeyData{
						KeyData:     m.keyData,
						Fingerprint: &waE2E.AppStateSyncKeyFingerprint{RawID: proto.Uint32(1)},
						Timestamp:   proto.Int64(Ago(90 * 24 * time.Hour).UnixMilli()),
					},
				}},
			},
		},
	}
}

// sendAppStateKey gives the client the key before anything asks it to decode a
// patch.
func (s *session) sendAppStateKey(ctx context.Context) error {
	plaintext, err := proto.Marshal(s.srv.appState.keyShareMessage())
	if err != nil {
		return fmt.Errorf("marshal app state key share: %w", err)
	}
	if err := s.sendSelfProtocolMessage(ctx, plaintext); err != nil {
		return err
	}
	s.srv.appState.noteShared()
	return nil
}

// memAppStateKeys is the key store the encoder needs. The mock holds one key
// for the life of the process.
type memAppStateKeys struct {
	mu     sync.Mutex
	keys   map[string]store.AppStateSyncKey
	latest []byte
}

func newMemAppStateKeys() *memAppStateKeys {
	return &memAppStateKeys{keys: map[string]store.AppStateSyncKey{}}
}

func (m *memAppStateKeys) PutAppStateSyncKey(_ context.Context, id []byte, key store.AppStateSyncKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[string(id)] = key
	m.latest = id
	return nil
}

func (m *memAppStateKeys) GetAppStateSyncKey(_ context.Context, id []byte) (*store.AppStateSyncKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key, ok := m.keys[string(id)]
	if !ok {
		return nil, nil
	}
	return &key, nil
}

func (m *memAppStateKeys) GetLatestAppStateSyncKeyID(context.Context) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.latest, nil
}

func (m *memAppStateKeys) GetAllAppStateSyncKeys(context.Context) ([]*store.AppStateSyncKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*store.AppStateSyncKey, 0, len(m.keys))
	for _, key := range m.keys {
		out = append(out, &key)
	}
	return out, nil
}

// memAppStateStore is the version and MAC store the encoder reads while
// chaining patches. Nothing outlives the process, which is the point.
type memAppStateStore struct {
	mu        sync.Mutex
	versions  map[string]uint64
	hashes    map[string][128]byte
	valueMACs map[string][]byte
}

func newMemAppStateStore() *memAppStateStore {
	return &memAppStateStore{
		versions:  map[string]uint64{},
		hashes:    map[string][128]byte{},
		valueMACs: map[string][]byte{},
	}
}

func (m *memAppStateStore) PutAppStateVersion(_ context.Context, name string, version uint64, hash [128]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.versions[name] = version
	m.hashes[name] = hash
	return nil
}

func (m *memAppStateStore) GetAppStateVersion(_ context.Context, name string) (uint64, [128]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.versions[name], m.hashes[name], nil
}

func (m *memAppStateStore) DeleteAppStateVersion(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.versions, name)
	delete(m.hashes, name)
	return nil
}

func (m *memAppStateStore) PutAppStateMutationMACs(_ context.Context, name string, _ uint64, mutations []store.AppStateMutationMAC) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mutation := range mutations {
		m.valueMACs[name+"/"+string(mutation.IndexMAC)] = mutation.ValueMAC
	}
	return nil
}

func (m *memAppStateStore) DeleteAppStateMutationMACs(_ context.Context, name string, indexMACs [][]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, indexMAC := range indexMACs {
		delete(m.valueMACs, name+"/"+string(indexMAC))
	}
	return nil
}

func (m *memAppStateStore) GetAppStateMutationMAC(_ context.Context, name string, indexMAC []byte) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.valueMACs[name+"/"+string(indexMAC)], nil
}

var _ store.AppStateSyncKeyStore = (*memAppStateKeys)(nil)
var _ store.AppStateStore = (*memAppStateStore)(nil)

// accept takes a patch the client sent, which is what happens when somebody
// pins or mutes a chat in the UI. Storing it is what makes the change survive
// the re-fetch whatsmeow does immediately afterwards, and the reconnect after
// that.
func (m *mockAppState) accept(ctx context.Context, w *World, name appstate.WAPatchName, encoded []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.applyPatch(ctx, w, name, encoded)
	return err
}

// applyMutations keeps the world's own idea of chat state in step with what the
// client just did, so a scenario reading it back sees what the UI shows.
func applyMutations(w *World, mutations []appstate.Mutation) {
	if w == nil {
		return
	}
	for _, mutation := range mutations {
		if len(mutation.Index) < 2 {
			continue
		}
		jid, err := types.ParseJID(mutation.Index[1])
		if err != nil {
			continue
		}
		chat, ok := w.chatByJID(jid)
		if !ok {
			continue
		}
		switch mutation.Index[0] {
		case appstate.IndexPin:
			chat.pinOrder = 0
			if mutation.Action.GetPinAction().GetPinned() {
				chat.pinOrder = 1
			}
		case appstate.IndexArchive:
			chat.archived = mutation.Action.GetArchiveChatAction().GetArchived()
		case appstate.IndexMute:
			chat.muted = mutation.Action.GetMuteAction().GetMuted()
		}
	}
}
