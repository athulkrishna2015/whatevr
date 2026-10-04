package ui

import v2 "github.com/codelif/whatevr/proto/whatevr/v2"

// What whattui reads off a row beyond its fields. The rows are the daemon's
// and read only; these are questions asked of them, never answers kept.

// editable reports whether an edit would still be taken at unix ms now. asked
// per keystroke and per frame, so it goes stale by itself.
func editable(m *v2.MessageRow, now int64) bool {
	return m.GetEditUntilMs() > 0 && now <= m.GetEditUntilMs()
}

// centred is a row nobody wrote: no bubble, no author, middle of the column.
func centred(m *v2.MessageRow) bool {
	switch m.WhichBody() {
	case v2.MessageRow_System_case, v2.MessageRow_CallLog_case:
		return true
	}
	return false
}

// plainText is a row whose words are all there is to it.
func plainText(m *v2.MessageRow) bool {
	return m.WhichBody() == v2.MessageRow_TextBody_case
}

// messageBody is what to draw as the message's words, text being the row's or
// the whole of it. a body whattui does not draw falls back to the daemon's
// one-liner, with the caption under it.
func messageBody(m *v2.MessageRow, text string) string {
	fallback := m.GetFallback()
	switch {
	case m.GetRevoked():
		return "This message was deleted"
	case m.HasSystem():
		return m.GetSystem().GetText()
	case plainText(m) && text != "":
		return text
	case text != "" && fallback != "":
		return fallback + "\n" + text
	case text != "":
		return text
	default:
		return fallback
	}
}

// statusOf is the delivery state, with a value this build has never heard of
// read as unspecified, as PROTOCOL.md says.
func statusOf(m *v2.MessageRow) v2.MessageStatus {
	s := m.GetStatus()
	if _, ok := v2.MessageStatus_name[int32(s)]; !ok {
		return v2.MessageStatus_MESSAGE_STATUS_UNSPECIFIED
	}
	return s
}

// connState is the connection's state, unknown values unspecified.
func connState(c *v2.ConnectionRow) v2.ConnectionState {
	s := c.GetState()
	if _, ok := v2.ConnectionState_name[int32(s)]; !ok {
		return v2.ConnectionState_CONNECTION_STATE_UNSPECIFIED
	}
	return s
}

// loginState is pairing's state, unknown values unspecified.
func loginState(l *v2.LoginRow) v2.LoginState {
	s := l.GetState()
	if _, ok := v2.LoginState_name[int32(s)]; !ok {
		return v2.LoginState_LOGIN_STATE_UNSPECIFIED
	}
	return s
}

// isGroup is a chat that names who wrote each message.
func isGroup(c *v2.ChatRow) bool {
	switch c.GetType() {
	case v2.ChatType_CHAT_TYPE_GROUP, v2.ChatType_CHAT_TYPE_COMMUNITY:
		return true
	}
	return false
}
