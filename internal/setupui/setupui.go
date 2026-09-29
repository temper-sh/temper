// Package setupui presents preset and layout choices without product effects.
package setupui

type Option struct {
	ID, Name, Description, DisabledReason string
	Details, AssessmentURL                string
	Components                            string
	Advice                                *Section
}

type Section struct {
	Title     string
	Lines     []string
	Downloads []Download // Non-nil sections have a collapsible file table.
	Warning   bool
}

type Download struct {
	File, Size, Status string
}
