package core

type SshKey struct {
	VendorID string
	Name     string
}

// SshKeySorter sorts SSH keys by VendorID ascending (lexicographic string compare).
// B2: this is a lex sort, not a numeric one — "v10" < "v2".
type SshKeySorter []SshKey

func (s SshKeySorter) Len() int           { return len(s) }
func (s SshKeySorter) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s SshKeySorter) Less(i, j int) bool { return s[i].VendorID < s[j].VendorID }
