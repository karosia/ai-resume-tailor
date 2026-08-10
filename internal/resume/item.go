package resume

type ItemType string

const (
	ItemExperience  ItemType = "experience"
	ItemAchievement ItemType = "achievement"
	ItemSkill       ItemType = "skill"
	ItemEducation   ItemType = "education"
	ItemProject     ItemType = "project"
)

func (t ItemType) valid() bool {
	switch t {
	case ItemExperience, ItemAchievement, ItemSkill, ItemEducation, ItemProject:
		return true
	default:
		return false
	}
}

type Item struct {
	ID        string   `json:"id"`
	Type      ItemType `json:"type"`
	Title     string   `json:"title"`
	Company   string   `json:"company,omitempty"`
	Content   string   `json:"content"`
	Skills    []string `json:"skills,omitempty"`
	Metrics   []string `json:"metrics,omitempty"`
	StartDate string   `json:"start_date,omitempty"`
	EndDate   string   `json:"end_date,omitempty"`
}
