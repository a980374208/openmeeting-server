package model

const (
	ContactSourceOrgColleague int32 = 1
	ContactSourcePersonalAdd  int32 = 2
)

const (
	ContactRequestPending  int32 = 0
	ContactRequestAccepted int32 = 1
	ContactRequestRejected int32 = 2
)

const (
	OrgEmployeeStatusActive  int32 = 1
	OrgEmployeeStatusRemoved int32 = 2
)

const (
	OrgMemberVisibilityInherit = ""
	OrgMemberVisibilityAll     = "all"
	OrgMemberVisibilityOwnDept = "own_dept"
)

func NormalizeOrgMemberVisibility(raw string) string {
	switch raw {
	case OrgMemberVisibilityOwnDept:
		return OrgMemberVisibilityOwnDept
	default:
		return OrgMemberVisibilityAll
	}
}

func NormalizeDeptMemberVisibility(raw string) string {
	switch raw {
	case OrgMemberVisibilityAll:
		return OrgMemberVisibilityAll
	case OrgMemberVisibilityOwnDept:
		return OrgMemberVisibilityOwnDept
	default:
		return OrgMemberVisibilityInherit
	}
}

type OrgDepartment struct {
	DeptID           string `bson:"dept_id"`
	DeptCode         string `bson:"dept_code,omitempty"`
	ParentID         string `bson:"parent_id"`
	Name             string `bson:"name"`
	AncestorPath     string `bson:"ancestor_path"` // e.g. "/root_id/sub_id/"
	SortOrder        int32  `bson:"sort_order"`
	MemberVisibility string `bson:"member_visibility"` // ""=inherit org default, "all", "own_dept"
	Version          int64  `bson:"version"`
	UpdatedAt        int64  `bson:"updated_at"`
}

func (d *OrgDepartment) EffectiveMemberVisibility(orgDefault string) string {
	if d != nil {
		if v := NormalizeDeptMemberVisibility(d.MemberVisibility); v != OrgMemberVisibilityInherit {
			return v
		}
	}
	return NormalizeOrgMemberVisibility(orgDefault)
}

type OrgEmployeeRecord struct {
	RecordID         string   `bson:"record_id"`
	OrgCode          string   `bson:"org_code,omitempty"`
	EmployeeNo       string   `bson:"employee_no"`
	UserID           string   `bson:"user_id"`
	Account          string   `bson:"account"`
	Nickname         string   `bson:"nickname"`
	JobTitle         string   `bson:"job_title,omitempty"`
	DeptNames        []string `bson:"dept_names,omitempty"`
	Status           int32    `bson:"status"`
	JoinedAt         int64    `bson:"joined_at"`
	RemovedAt        int64    `bson:"removed_at,omitempty"`
	RemovedReason    string   `bson:"removed_reason,omitempty"`
	RemovedByUserID  string   `bson:"removed_by_user_id,omitempty"`
	RemovedByAccount string   `bson:"removed_by_account,omitempty"`
	UpdatedAt        int64    `bson:"updated_at"`
}

type UserDepartmentMember struct {
	DeptID    string `bson:"dept_id"`
	UserID    string `bson:"user_id"`
	IsLeader  bool   `bson:"is_leader"`
	SortOrder int32  `bson:"sort_order"`
	UpdatedAt int64  `bson:"updated_at"`
}

type ContactRelation struct {
	OwnerUserID  string   `bson:"owner_user_id"`
	TargetUserID string   `bson:"target_user_id"`
	Remark       string   `bson:"remark"`
	IsFavorite   bool     `bson:"is_favorite"`
	IsBlocked    bool     `bson:"is_blocked"`
	GroupIDs     []string `bson:"group_ids"`
	SourceType   int32    `bson:"source_type"`
	CreatedAt    int64    `bson:"created_at"`
	UpdatedAt    int64    `bson:"updated_at"`
}

type ContactGroup struct {
	GroupID     string `bson:"group_id"`
	OwnerUserID string `bson:"owner_user_id"`
	Name        string `bson:"name"`
	SortOrder   int32  `bson:"sort_order"`
	UpdatedAt   int64  `bson:"updated_at"`
}

type ContactRequest struct {
	RequestID  string `bson:"request_id"`
	FromUserID string `bson:"from_user_id"`
	ToUserID   string `bson:"to_user_id"`
	Greeting   string `bson:"greeting"`
	Status     int32  `bson:"status"`
	CreatedAt  int64  `bson:"created_at"`
	HandledAt  int64  `bson:"handled_at"`
}
