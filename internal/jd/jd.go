package jd

type JD struct {
	Title            string   `json:"title"`
	Seniority        string   `json:"seniority"`
	RequiredSkills   []string `json:"required_skills"`
	NiceToHave       []string `json:"nice_to_have"`
	Keywords         []string `json:"keywords"`
	Responsibilities []string `json:"responsibilities"`
}
