package protocol

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"whatevrd/internal/wa"
)

// The development command surface. It is deliberately **not** in PROTOCOL.md:
// nothing here is part of the contract between the daemon and a frontend, and
// none of it is registered unless WHATEVR_DEV_COMMANDS is set. A frontend that
// depended on it would be depending on a debugging tool.

// DevEnvVar gates the whole surface. Anything other than "1" leaves the daemon
// with exactly the commands the protocol document describes.
const DevEnvVar = "WHATEVR_DEV_COMMANDS"

// DevActions is the seam behind the development commands. It is separate from
// CommandActions so the production interface (and every fake implementing it)
// stays untouched by a debugging tool.
type DevActions interface {
	SendRawMessage(ctx context.Context, req wa.RawSendRequest) (string, error)
}

// DevCommandsEnabled reports whether the environment asked for them.
func DevCommandsEnabled() bool {
	return strings.TrimSpace(os.Getenv(DevEnvVar)) == "1"
}

// RegisterDevCommands adds the development surface. Callers should guard with
// DevCommandsEnabled; the check is repeated here so a mistaken call cannot open
// the surface by accident.
func RegisterDevCommands(s *Server, actions DevActions) {
	if !DevCommandsEnabled() || actions == nil {
		return
	}
	h := devHandlers{actions: actions}
	s.RegisterCommand("dev.send_raw", backgroundNet(h.sendRaw, false))
}

type devHandlers struct {
	actions DevActions
}

// sendRaw builds one message of a named kind and pushes it through the real
// ingest, either after really sending it or purely locally. Section 1 is about
// receiving, and this is what makes the receive path drivable without a second
// handset in hand for every iteration.
func (h devHandlers) sendRaw(ctx context.Context, _ *conn, req request) (any, *Error) {
	var p wa.RawSendRequest
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, errorf(CodeInvalidParams, "malformed dev.send_raw params")
		}
	}
	if strings.TrimSpace(p.ChatID) == "" {
		return nil, errorf(CodeInvalidParams, "dev.send_raw needs a chat_id")
	}
	if strings.TrimSpace(p.Kind) == "" {
		return nil, errorf(CodeInvalidParams, "dev.send_raw needs a kind (one of: %s)", strings.Join(wa.RawSendKinds(), ", "))
	}
	messageID, err := h.actions.SendRawMessage(ctx, p)
	if perr := mapCommandError(err); perr != nil {
		return nil, perr
	}
	return map[string]any{"message_id": messageID}, nil
}
