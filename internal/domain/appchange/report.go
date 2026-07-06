package appchange

type Mode string

const (
	ModeNone           Mode = "none"
	ModeAdapter        Mode = "adapter"
	ModeGeneratedPatch Mode = "generated_patch"
	ModeManualReview   Mode = "manual_review"
)

type Change struct {
	Service       string `json:"service"`
	ResourceID    string `json:"resource_id"`
	Mode          Mode   `json:"mode"`
	Reason        string `json:"reason"`
	AdapterURL    string `json:"adapter_url,omitempty"`
	File          string `json:"file,omitempty"`
	Search        string `json:"search,omitempty"`
	Replace       string `json:"replace,omitempty"`
	ValidationCmd string `json:"validation_cmd,omitempty"`
}

type Report struct {
	Changes []Change `json:"changes"`
}

func (r Report) RequiresAction() bool {
	for _, change := range r.Changes {
		if change.Mode == ModeGeneratedPatch || change.Mode == ModeManualReview {
			return true
		}
	}
	return false
}
