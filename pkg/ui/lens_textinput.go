package ui

// IsPrintableKey returns true if the key is a printable ASCII character.
// This is used by text input handlers to filter which keys to append.
func IsPrintableKey(key string) bool {
	return len(key) == 1 && key[0] >= 32 && key[0] < 127
}
