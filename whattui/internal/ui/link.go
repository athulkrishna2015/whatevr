package ui

import "strings"

// Link detection, deliberately small. A message body is not html and the
// daemon does not mark up urls, so this is whattui's own guess, and a guess
// that is too clever turns ordinary punctuation into a broken link.

var schemes = []string{"https://", "http://", "www."}

// trailing is punctuation a sentence puts after a url rather than in it.
const trailing = ".,;:!?)]}'\"…»"

// linkSpans cuts one whitespace-delimited word into the text before a url, the
// url, and the text after it. A word with no url in it is one span.
func linkSpans(word string) line {
	at, scheme := -1, ""
	for _, s := range schemes {
		if i := strings.Index(word, s); i >= 0 && (at < 0 || i < at) {
			at, scheme = i, s
		}
	}
	if at < 0 {
		return line{{text: word}}
	}
	end := len(word)
	for end > at+len(scheme) && strings.ContainsRune(trailing, rune(word[end-1])) {
		end--
	}
	// A scheme with nothing after it is somebody typing about urls.
	if end <= at+len(scheme) {
		return line{{text: word}}
	}

	url := word[at:end]
	href := url
	if scheme == "www." {
		// A bare host is still a link, and a terminal will not open one
		// without a scheme to open it with.
		href = "https://" + url
	}

	var out line
	out = appendSpan(out, span{text: word[:at]})
	out = appendSpan(out, span{text: url, link: href})
	out = appendSpan(out, span{text: word[end:]})
	return out
}
