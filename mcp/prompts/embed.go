package prompts

import _ "embed"

//go:embed categorize.txt
var Categorize string

//go:embed summarize.txt
var Summarize string

//go:embed session.txt
var Session string

//go:embed reminder.txt
var Reminder string

//go:embed enrich-meeting.txt
var EnrichMeeting string

//go:embed generate-requirements.txt
var GenerateRequirements string
