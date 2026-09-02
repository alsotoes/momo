package model

type Spec struct {
	ID         string
	Status     string // Accepted, Proposed, Deprecated
	Issue      string
	Proposal   Proposal
	Spec       SpecDoc
	Tasks      Tasks
	Supersedes string
}

type Proposal struct {
	Why         string
	WhatChanges string
	NonGoals    string
}

type SpecDoc struct {
	Requirements []Requirement
	Consequences string
	Alternatives []Alternative
}

type Requirement struct {
	Title     string
	Summary   string
	Scenarios []Scenario
}

type Scenario struct {
	Name  string
	Given string
	When  string
	Then  string
}

type Alternative struct {
	Label       string
	Description string
	Pros        []string
	Cons        []string
}

type Tasks struct {
	Categories map[string][]TaskItem
}

type TaskItem struct {
	Text     string
	Done     bool
	Category string // Code, Tests, Docs
}

type ADR struct {
	Number         int
	SpecID         string
	Status         string
	Confidence     string
	Context        string
	Decision       string
	Consequences   string
	Alternatives   []Alternative
	Implementation Implementation
	References     References
	Supersedes     string
	SupersededBy   string
}

type Implementation struct {
	Code  Status
	Tests Status
	Docs  Status
	Blog  string
}

type References struct {
	Issue        string
	PR           string
	Spec         string
	Blog         string
	Supersedes   string
	SupersededBy string
}

type Status string

const (
	StatusDone    Status = "Done"
	StatusPartial Status = "Partial"
	StatusPlanned Status = "Planned"
)

type Confidence string

const (
	ConfidenceHigh   Confidence = "High"
	ConfidenceMedium Confidence = "Medium"
	ConfidenceLow    Confidence = "Low"
)
