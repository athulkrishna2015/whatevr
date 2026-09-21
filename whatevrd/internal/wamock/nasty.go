//go:build whatevr_mock

package wamock

import "strings"

// The corpus of text that breaks interfaces. Every entry is a class of failure
// somebody has actually shipped: a terminal that executes what it renders, a
// wrap that splits a grapheme, a column that counts runes instead of cells.
//
// Invisible characters are written as escapes on purpose. A literal zero width
// joiner in this file is a character nobody can see in a diff, and a corpus
// nobody can review is not a corpus.

type nastyText struct {
	Label string
	Text  string
}

// zalgo stacks combining marks on one base letter. A renderer that gives every
// combining mark its own cell turns this into a line of confetti; one that
// clips to the cell smears it over the row above.
func zalgo(base string, depth int) string {
	marks := []rune{0x0300, 0x0301, 0x0302, 0x0303, 0x0304, 0x0306, 0x0307, 0x0308,
		0x030A, 0x030B, 0x030C, 0x0327, 0x0328, 0x0332, 0x0333, 0x0345}
	var b strings.Builder
	for _, r := range base {
		b.WriteRune(r)
		for i := 0; i < depth; i++ {
			b.WriteRune(marks[i%len(marks)])
		}
	}
	return b.String()
}

// nastyTexts is the whole sheet, in the order a reader should meet it.
func nastyTexts() []nastyText {
	return []nastyText{
		{"empty", ""},
		{"whitespace only", "   \t  \t "},
		{"one emoji", "\U0001F389"},
		{"zwj families and modifiers", "\U0001F468\u200d\U0001F469\u200d\U0001F467\u200d\U0001F466 \U0001F469\U0001F3FD\u200d\U0001F680 \U0001F9D1\U0001F3FF\u200d\U0001F9B0 \U0001F3F3\ufe0f\u200d\U0001F308 \U0001F1EE\U0001F1F3\U0001F1EF\U0001F1F5 1\ufe0f\u20e3 ❤\ufe0f\u200d\U0001F525 \U0001F44D\U0001F3FE"},
		{"emoji wall", strings.Repeat("\U0001F600\U0001F648\U0001F680\U0001F9EC\U0001FAE0", 40)},
		{"rtl in ltr", "شكرا! the report is at /home/harsh/التقرير.pdf, ok?"},
		{"hebrew with digits", "הקובץ 42 נמצא ב-/var/log/syslog.1 בשעה 14:30"},
		{"bidi override", "the file is called \u202egpj.exe\u202c, careful"},
		{"bidi isolates", "\u2066left\u2069 \u2067right\u2069 \u2068neutral\u2069 back to normal"},
		{"cjk and halfwidth", "日本語のテキストと한국어와中文が混ざるとｱｲｳｴｵ幅がずれる"},
		{"thai, no word breaks", "ภาษาไทยไม่มีช่องว่างระหว่างคำและมีสระอยู่ทั้งบนและล่างของบรรทัด"},
		{"devanagari conjuncts", "क्षत्रिय श्रृंगार द्वंद्व ज्ञान ट्रैक्टर हिन्दी में लिखा हुआ वाक्य"},
		{"arabic shaping", "مرحبا بكم في اختبار الواجهة العربية المتصلة الحروف"},
		{"zalgo", zalgo("breaking", 12)},
		{"combining on one cell", "ȩ̱́̈̊̃ á́́́́"},
		{"invisible characters", "a\u200bb\u200cc\u200dd\u00ade\u00a0f\u2060g\ufeffh"},
		{"line separators", "before\u2028after\u0085next\u2029last"},
		{"ansi colour escape", "\x1b[31mred\x1b[0m \x1b[1;44mbold on blue\x1b[0m plain"},
		{"osc 8 hyperlink injection", "\x1b]8;;https://evil.example\x1b\\innocent text\x1b]8;;\x1b\\"},
		{"osc 52 clipboard write", "\x1b]52;c;aGVsbG8gZnJvbSBhIG1lc3NhZ2U=\x07 did that set your clipboard?"},
		{"cursor movement", "top\x1b[2A\x1b[10Cmoved\x1b[H home"},
		{"screen clear", "before\x1b[2Jafter\x1bc reset"},
		{"c0 controls", "bell\x07 backspace\x08 formfeed\x0c vtab\x0b delete\x7f end"},
		{"nul byte", "before\x00after"},
		{"c1 controls", "\u009b31m csi \u0085 nel \u009d osc \u009c end"},
		{"tabs", "col1\tcol2\t\tcol3\t\t\tcol4"},
		{"carriage returns", "first line\rovertyped\rlast"},
		{"newline storm", strings.Repeat("line\n", 60)},
		{"one long word", strings.Repeat("unbreakable", 60)},
		{"long url", "https://example.invalid/" + strings.Repeat("segment/", 80) + "?q=" + strings.Repeat("x", 200)},
		{"many urls", strings.Repeat("https://a.example https://b.example/path#frag ", 12)},
		{"idn homograph", "log in at https://аpple.com and https://apple.com, spot the difference"},
		{"url punctuation", "see (https://example.com/a), or <https://example.com/b>, or https://example.com/c."},
		{"whatsapp formatting", "*bold* _italic_ ~strike~ ```code``` and *_nested_* and a stray * asterisk"},
		{"fullwidth", "ＦＵＬＬＷＩＤＴＨ　ＴＥＸＴ　１２３４５６７８９０"},
		{"math alphanumerics", "\U0001D4EF\U0001D4EA\U0001D4F7\U0001D4EC\U0001D4FE \U0001D565\U0001D556\U0001D569\U0001D565 \U0001F132\U0001F138\U0001F141\U0001F132\U0001F13B\U0001F134"},
		{"replacement and private use", "\ufffd \ue000\uf8ff \U000F0000 end"},
		{"variation selectors", "❤\ufe0f vs ❤︎, ⚠\ufe0f vs ⚠︎"},
		{"ideographic space", "間　隔　が　広　い"},
		{"mixed everything", "\U0001F680 सर, ‮تقرير‬ *urgent* https://example.com/\u200bpath ok?\t\x1b[0m"},
	}
}

// hugeMessage is the single message nobody budgeted for. Forty thousand
// characters is past every buffer a transcript is likely to have and still
// small enough that a scenario builds instantly.
func hugeMessage() string {
	var b strings.Builder
	b.Grow(41000)
	b.WriteString("a very long message follows, and it has to wrap, scroll and select correctly the whole way down: ")
	for i := 0; b.Len() < 40000; i++ {
		b.WriteString("paragraph ")
		b.WriteString(strings.Repeat("filler ", 20))
		if i%7 == 0 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

// nastyNames are what a chat can be called. A name is drawn in a fixed column
// with a badge after it, so anything that measures wrong here misaligns a whole
// list rather than one row.
func nastyNames() []string {
	return []string{
		"\U0001F389",
		"مجموعة الاختبار",
		"日本語のグループ名前が長すぎる場合の折り返し",
		"नाम " + zalgo("गड़बड़", 8),
		strings.Repeat("a very long group name ", 18),
		"name\nwith\nnewlines",
		"name\twith\ttabs",
		"\x1b[31mred name\x1b[0m",
		"   leading and trailing   ",
		"\U0001F468\u200d\U0001F469\u200d\U0001F467\u200d\U0001F466\U0001F468\u200d\U0001F469\u200d\U0001F467\u200d\U0001F466",
		"\u202ereversed name\u202c",
		"ＷＩＤＥ　ＮＡＭＥ",
	}
}
