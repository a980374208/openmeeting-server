package definition

type User struct {
	Account    string `json:"account" column:"account"`
	Nickname   string `json:"nickname" column:"nickname"`
	Password   string `json:"password" column:"password"`
	Birth      string `json:"birth" column:"birth"`
	Gender     string `json:"gender" column:"gender"`
	Email      string `json:"email" column:"email"`
	Phone      string `json:"phone" column:"phone"`
	EmployeeNo string `json:"employeeNo" column:"employee_no"`
	JobTitle   string `json:"jobTitle" column:"job_title"`
	Department string `json:"department" column:"department"` // e.g. "研发中心/音视频组"
	IsLeader   string `json:"isLeader" column:"is_leader"`    // "true" / "1"
	Signature  string `json:"signature" column:"signature"`
	AvatarURL  string `json:"avatarURL" column:"avatar_url"`
}

func (User) SheetName() string {
	return "user"
}
