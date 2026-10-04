package view

import v2 "github.com/codelif/whatevr/proto/whatevr/v2"

// the row extractors whattui's views use

func Chat(u *v2.Upsert) (*v2.ChatRow, bool)             { return u.GetChat(), u.HasChat() }
func Message(u *v2.Upsert) (*v2.MessageRow, bool)       { return u.GetMessage(), u.HasMessage() }
func Connection(u *v2.Upsert) (*v2.ConnectionRow, bool) { return u.GetConnection(), u.HasConnection() }
func Login(u *v2.Upsert) (*v2.LoginRow, bool)           { return u.GetLogin(), u.HasLogin() }
func Self(u *v2.Upsert) (*v2.SelfRow, bool)             { return u.GetSelf(), u.HasSelf() }
func Sync(u *v2.Upsert) (*v2.SyncRow, bool)             { return u.GetSync(), u.HasSync() }
func Problem(u *v2.Upsert) (*v2.ProblemRow, bool)       { return u.GetProblem(), u.HasProblem() }
func Reaction(u *v2.Upsert) (*v2.Reaction, bool)        { return u.GetReaction(), u.HasReaction() }
