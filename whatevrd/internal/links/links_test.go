package links

import "testing"

func TestParse(t *testing.T) {
	good := map[string]Link{
		"whatevr://":                       {Kind: Activate},
		"whatevr:":                         {Kind: Activate},
		"WHATEVR-DEV://":                   {Kind: Activate},
		"whatevr://chat/c_abc":             {Kind: Chat, ChatID: "c_abc"},
		"whatevr://chat/c%3Aabc/":          {Kind: Chat, ChatID: "c:abc"},
		"whatevr:chat/c_abc":               {Kind: Chat, ChatID: "c_abc"},
		"whatevr://chat?phone=%2B911234":   {Kind: Address, Phone: "+911234"},
		"whatevr://chat?lid=123":           {Kind: Address, LID: "123"},
		"whatevr://chat/?username=someone": {Kind: Address, Username: "someone"},
	}
	for in, want := range good {
		got, err := Parse(in)
		if err != nil || got != want {
			t.Errorf("%s: %+v %v, want %+v", in, got, err, want)
		}
	}
	for _, in := range []string{
		"https://chat/x",
		"whatsapp://send?phone=1",
		"whatevr://send?phone=1&text=hi",
		"whatevr://chat",
		"whatevr://chat/a/b",
		"whatevr://chat/x?phone=1",
		"whatevr://chat?phone=1&lid=2",
		"whatevr://chat?phone=1&phone=2",
		"whatevr://chat?text=hi",
		"whatevr://chat?phone=",
		"whatevr://user@chat/x",
		"whatevr://chat/x#frag",
		"whatevr://?x=1",
	} {
		if l, err := Parse(in); err == nil {
			t.Errorf("%s parsed as %+v", in, l)
		}
	}
}
