package core

//Server main server model
type Server struct {
	VendorID string
	Name     string
	Age      float64
	Tags     []string
	Region   string
	State    string // "RUNNING|TERMINATED"
}

//ServerSorter sorts servers by age.
type ServerSorter []Server

func (s ServerSorter) Len() int           { return len(s) }
func (s ServerSorter) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s ServerSorter) Less(i, j int) bool { return s[i].Age > s[j].Age }
