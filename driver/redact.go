package moonraker

import (
	"errors"
	"sort"
	"strings"
)

// authorityBounds finds the authority of an address by string operations, so
// it works on input url.Parse rejects. It returns the start and end offsets.
func authorityBounds(address string) (int, int) {
	start := 0
	if i := strings.Index(address, "://"); i >= 0 {
		start = i + len("://")
	}

	end := len(address)
	if i := strings.IndexAny(address[start:], "/?#"); i >= 0 {
		end = start + i
	}

	return start, end
}

// userinfoOf returns the userinfo of an address with its trailing @, or "".
func userinfoOf(address string) string {
	start, end := authorityBounds(address)

	at := strings.LastIndex(address[start:end], "@")
	if at < 0 {
		return ""
	}

	return address[start : start+at+1]
}

// redactAddress removes the userinfo from an address of any shape.
func redactAddress(address string) string {
	info := userinfoOf(address)
	if info == "" {
		return address
	}

	start, _ := authorityBounds(address)

	return address[:start] + address[start+len(info):]
}

// redactCause returns a new error, wrapping nothing, whose text is err's with
// the address, the target and their userinfo removed.
func redactCause(err error, address, target string) error {
	text := err.Error()
	trimmed := strings.TrimSpace(address)
	infos := []string{userinfoOf(address), userinfoOf(trimmed), userinfoOf(target)}

	for _, full := range []string{target, address, trimmed} {
		if full != "" {
			text = strings.ReplaceAll(text, full, redactAddress(full))
		}
	}

	sort.Slice(infos, func(i, j int) bool { return len(infos[i]) > len(infos[j]) })

	for _, info := range infos {
		if info != "" {
			text = strings.ReplaceAll(text, info, "")
		}
	}

	return errors.New(text) //nolint:err113 // the cause is only its redacted text
}
