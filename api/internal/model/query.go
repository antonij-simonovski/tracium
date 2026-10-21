package model

// QueryResult is a custom query's answer: Columns orders the keys of each row.
type QueryResult struct {
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
	Meta    QueryMeta        `json:"meta"`
}

// QueryMeta echoes the resolved window and says where cost came from:
// "reconciled" matches the dashboard's cost KPI, "spans" counts span cost only
// (usage-metric rows carry no workflow, model or attribute to group by).
type QueryMeta struct {
	View        string `json:"view"`
	From        string `json:"from"`
	To          string `json:"to"`
	Granularity string `json:"granularity,omitempty"`
	CostSource  string `json:"cost_source,omitempty"`
	Truncated   bool   `json:"truncated"`
	Page        int    `json:"page"`
	// NextPage is set when more rows match; pass it as page to read them.
	NextPage int `json:"next_page,omitempty"`
}

// QuerySchema is what a caller can ask for: the static catalog plus the
// attribute keys, workflows and models present in its own data.
type QuerySchema struct {
	Views []QueryView `json:"views"`
	// MetricFunctions build a metric from any field as <func>:<field>.
	MetricFunctions []string `json:"metric_functions"`
	FilterOps       []string `json:"filter_ops"`
	HavingOps       []string `json:"having_ops"`
	Granularities   []string `json:"granularities"`
	AttributeKeys   []string `json:"attribute_keys"`
	Workflows       []string `json:"workflows"`
	Models          []string `json:"models"`
	MaxWindowDays   int      `json:"max_window_days"`
	MaxLimit        int      `json:"max_limit"`
	MaxRowLimit     int      `json:"max_row_limit"`
}

type QueryView struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Dimensions  []QueryField `json:"dimensions"`
	Metrics     []QueryField `json:"metrics"`
	// Columns are the fields usable beside the dimensions: per-run values on
	// runs, live stored columns on calls.
	Columns []QueryColumn `json:"columns"`
	// Related are prefixed fields reaching other rows, e.g. run.<field>.
	Related []QueryField `json:"related"`
}

// QueryColumn is a field beside the dimensions; Kind is number or text.
type QueryColumn struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description,omitempty"`
}

type QueryField struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
