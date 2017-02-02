package core

type SshKey struct {
	VendorID string
	Name     string
}

//SshKeyNameSorter sorts SSH keys by Name
type SshKeySorter []SshKey

func (s SshKeySorter) Len() int           { return len(s) }
func (s SshKeySorter) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s SshKeySorter) Less(i, j int) bool { return s[i].VendorID < s[j].VendorID }
