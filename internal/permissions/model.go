package permissions

type Grant struct {
	SubjectID  string
	Capability string
	Scope      string
}

type Request struct {
	SubjectID  string
	Capability string
	Scope      string
}

type Decision struct {
	Allowed bool
	Reason  string
}
