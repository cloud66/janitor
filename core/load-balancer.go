package core

//LoadBalancer main server model
type LoadBalancer struct {
	Name          string
	Age           float64
	InstanceCount int
	Region        string
}

//LoadBalancerSorter sorts load balancers by name
type LoadBalancerSorter []LoadBalancer

func (s LoadBalancerSorter) Len() int           { return len(s) }
func (s LoadBalancerSorter) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s LoadBalancerSorter) Less(i, j int) bool { return s[i].Name > s[j].Name }
