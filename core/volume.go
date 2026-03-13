package core

//Volume represents a block storage volume
type Volume struct {
	VendorID string
	Name     string
	Age      float64 // age in days
	Region   string
}

//VolumeSorter sorts volumes by age (oldest first)
type VolumeSorter []Volume

func (s VolumeSorter) Len() int           { return len(s) }
func (s VolumeSorter) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s VolumeSorter) Less(i, j int) bool { return s[i].Age > s[j].Age }
