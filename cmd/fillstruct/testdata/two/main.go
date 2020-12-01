package two

type s1 struct {
	a int
	b chan bool
	c string
}

type s2 struct {
	a int
	b bool
	c chan struct{}
}

var _, _ = s1{}, s2{}
