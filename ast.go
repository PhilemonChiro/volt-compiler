package compiler

// AST node definitions, ported from ast_nodes.py. Type names and field order
// match the Python dataclasses exactly: the golden AST dumps (DumpAST) are
// keyed on them. The `json` tag is the Python field name; `volt:"extra"`
// marks attributes Python sets outside the dataclass fields (dumped after
// the fields, sorted, and only when non-nil).

// Node is implemented by every AST node.
type Node interface{ base() *Base }

// Base carries the source region every node has.
type Base struct {
	Region Region `json:"region"`
}

func (b *Base) base() *Base { return b }

// NodeRegion returns a node's source region.
func NodeRegion(n Node) Region { return n.base().Region }

// Dict is an insertion-ordered map, like a Python dict: re-setting a key
// keeps its original position.
type Dict struct {
	Keys []string
	Vals map[string]any
}

func (d *Dict) Set(k string, v any) {
	if d.Vals == nil {
		d.Vals = map[string]any{}
	}
	if _, ok := d.Vals[k]; !ok {
		d.Keys = append(d.Keys, k)
	}
	d.Vals[k] = v
}

// --- Program (root) ---

type Program struct {
	Base
	Chip        *ChipDecl      `json:"chip"`
	Hardware    *HardwareBlock `json:"hardware"`
	Power       *PowerBlock    `json:"power"`
	Structs     []*StructDecl  `json:"structs"`
	Enums       []*EnumDecl    `json:"enums"`
	Functions   []*FuncDecl    `json:"functions"`
	Tasks       []*TaskDecl    `json:"tasks"`
	Machines    []*MachineDecl `json:"machines"`
	StorageVars []*StorageDecl `json:"storage_vars"`
	GlobalVars  []*VarDecl     `json:"global_vars"`
	Channels    []*ChannelDecl `json:"channels"`
	Setup       *SetupBlock    `json:"setup"`
	Loop        *LoopBlock     `json:"loop"`
	RawBlocks   []*RawBlock    `json:"raw_blocks"`
	Includes    []string       `json:"includes"`
}

// --- Top-level declarations ---

type ChipDecl struct {
	Base
	Target string `json:"target"`
}

type HardwareBlock struct {
	Base
	Pins    []*PinDecl    `json:"pins"`
	Buses   []*BusDecl    `json:"buses"`
	Sensors []*SensorDecl `json:"sensors"`
	// normalized: implicit buses and default addresses are filled in.
	normalized bool `json:"-"`
}

type PinDecl struct {
	Base
	Name string `json:"name"`
	Mode string `json:"mode"` // "Output", "Input", ... ("" = inferred)
	GPIO int    `json:"gpio"`
	// GPIORegion spans the pin number in the source (for quick fixes).
	GPIORegion Region `json:"-"`
}

type BusDecl struct {
	Base
	BusType string `json:"bus_type"`
	Name    string `json:"name"`
	Params  Dict   `json:"params"` // name -> Node
	// Implicit marks a bus the compiler made for devices declared without
	// `on` (chip-native pins, or a UART device's own port). Never printed.
	Implicit bool `json:"-"`
}

// SensorDecl is a device: `device x: T(p: v, ...) on bus at addr`, every
// part optional. PinParams holds the named args (int, or Node if not an int
// literal); BusName/Address are filled in for implicit buses and default
// addresses before the hardware checks.
type SensorDecl struct {
	Base
	Name       string `json:"name"`
	SensorType string `json:"sensor_type"`
	BusName    string `json:"bus_name"`
	Address    int    `json:"address"`
	// BusRegion spans the `on` bus name (rename / find references).
	BusRegion Region `json:"-"`
	// AddressRegion spans the `at` expression (for quick fixes).
	AddressRegion Region `json:"-"`
	PinParams     Dict   `json:"pin_params"`
	// ArgRegions spans each named arg's value (pin checks, quick fixes).
	ArgRegions map[string]Region `json:"-"`
	// HasAddress / HasBus: `at` / `on` were written.
	HasAddress bool `json:"-"`
	HasBus     bool `json:"-"`
	// TypeRegion spans the device type name.
	TypeRegion Region `json:"-"`
	// EndRegion is the empty span just past the declaration (insert fixes).
	EndRegion Region `json:"-"`
}

type PowerBlock struct {
	Base
	Items Dict `json:"items"` // name -> string
}

type StructDecl struct {
	Base
	Name   string       `json:"name"`
	Fields []*FieldDecl `json:"fields"`
}

type EnumDecl struct {
	Base
	Name     string   `json:"name"`
	Variants []string `json:"variants"`
}

type FieldDecl struct {
	Base
	Name     string `json:"name"`
	TypeName string `json:"type_name"`
}

type FuncDecl struct {
	Base
	Name          string   `json:"name"`
	Params        []*Param `json:"params"`
	ReturnType    *string  `json:"return_type"` // nil = void
	Body          []Node   `json:"body"`
	ContainsYield bool     `json:"contains_yield"`
}

type Param struct {
	Base
	Name     string `json:"name"`
	TypeName string `json:"type_name"`
}

type TaskDecl struct {
	Base
	Name         string   `json:"name"`
	Body         []Node   `json:"body"`
	Tier         string   `json:"tier"`
	CrossingVars []string `json:"crossing_vars"`
	HasStrict    bool     `json:"has_strict"`
	// Set (possibly empty) for declared tasks; nil for tasks made from
	// top-level every/on blocks.
	Annotations []string `json:"_annotations" volt:"extra"`
}

type SetupBlock struct {
	Base
	Body []Node `json:"body"`
}

type LoopBlock struct {
	Base
	Body []Node `json:"body"`
}

type BreakStmt struct{ Base }

type BecomeStmt struct {
	Base
	Target string `json:"target"`
}

type ContinueStmt struct{ Base }

type CompoundAssign struct {
	Base
	Target Node   `json:"target"`
	Op     string `json:"op"`
	Value  Node   `json:"value"`
}

type RawBlock struct {
	Base
	CCode string `json:"c_code"`
}

type OrExpr struct {
	Base
	Expr     Node   `json:"expr"`
	Fallback Node   `json:"fallback"`
	Kind     string `json:"kind"` // "skip", "restart", "retry", "default"
}

type StorageDecl struct {
	Base
	Name         string `json:"name"`
	TypeName     string `json:"type_name"`
	DefaultValue Node   `json:"default_value"`
}

// --- State machines ---

type MachineDecl struct {
	Base
	Name      string       `json:"name"`
	States    []*StateDecl `json:"states"`
	Variables []*VarDecl   `json:"variables"`
}

type StateDecl struct {
	Base
	Name             string             `json:"name"`
	IsInitial        bool               `json:"is_initial"`
	EnterBody        []Node             `json:"enter_body"`
	ExitBody         []Node             `json:"exit_body"`
	Transitions      []*StateTransition `json:"transitions"`
	TimedTransitions []*TimedTransition `json:"timed_transitions"`
	Body             []Node             `json:"body"`
}

type StateTransition struct {
	Base
	Event       Node   `json:"event"`
	TargetState string `json:"target_state"`
	Guard       Node   `json:"guard"`
	Action      []Node `json:"action"`
}

type TimedTransition struct {
	Base
	Duration    Node   `json:"duration"`
	TargetState string `json:"target_state"`
	Guard       Node   `json:"guard"`
	Action      []Node `json:"action"`
}

// --- Statements ---

type VarDecl struct {
	Base
	Name         string  `json:"name"`
	TypeName     *string `json:"type_name"` // nil = inferred
	Value        Node    `json:"value"`
	Mutable      bool    `json:"mutable"`
	Shared       bool    `json:"shared"`
	NeedsLifting bool    `json:"needs_lifting"`
}

type ChannelDecl struct {
	Base
	Name     string `json:"name"`
	ElemType string `json:"elem_type"`
	Capacity int    `json:"capacity"`
}

type AssignExpr struct {
	Base
	Target Node `json:"target"`
	Value  Node `json:"value"`
}

type ReturnStmt struct {
	Base
	Value Node `json:"value"`
}

type ExprStmt struct {
	Base
	Expr Node `json:"expr"`
	// Set by the transformer on sleep()/deep_sleep()/light_sleep() calls.
	YieldLabel *int `json:"_yield_label" volt:"extra"`
}

type IfStmt struct {
	Base
	Condition Node   `json:"condition"`
	ThenBody  []Node `json:"then_body"`
	ElseBody  any    `json:"else_body"` // nil, []Node, or *IfStmt (else if)
}

type WhileStmt struct {
	Base
	Condition Node   `json:"condition"`
	Body      []Node `json:"body"`
}

type ForStmt struct {
	Base
	VarName string `json:"var_name"`
	Start   Node   `json:"start"`
	End     Node   `json:"end"`
	Body    []Node `json:"body"`
	Step    Node   `json:"step"`
}

type EveryBlock struct {
	Base
	Interval   Node   `json:"interval"`
	Strict     bool   `json:"strict"`
	Guard      Node   `json:"guard"`
	Body       []Node `json:"body"`
	YieldLabel int    `json:"yield_label"`
}

type OnBlock struct {
	Base
	Event         Node   `json:"event"`
	Debounce      Node   `json:"debounce"`
	Throttle      Node   `json:"throttle"`
	Body          []Node `json:"body"`
	YieldLabel    int    `json:"yield_label"`
	DebounceLabel int    `json:"debounce_label"`
}

type TimeoutBlock struct {
	Base
	Duration     Node   `json:"duration"`
	TryBody      []Node `json:"try_body"`
	FallbackBody []Node `json:"fallback_body"`
	YieldLabel   *int   `json:"_yield_label" volt:"extra"` // set by the transformer
}

// TryBlock is `try { ... } or <policy>`. Terminal is set iff RetryLimit is.
type TryBlock struct {
	Base
	Body             []Node  `json:"body"`
	Policy           string  `json:"policy"` // "retry", "skip", "restart"
	RetryLimit       *int    `json:"retry_limit"`
	Terminal         *string `json:"terminal"` // "skip", "restart", "fail"
	TryID            int     `json:"try_id"`
	NeedsContextLift bool    `json:"needs_context_lift"`
	IsYieldPoint     bool    `json:"is_yield_point"`
}

type MatchStmt struct {
	Base
	Expr Node        `json:"expr"`
	Arms []*MatchArm `json:"arms"`
}

type MatchArm struct {
	Base
	Pattern string `json:"pattern"`
	Body    any    `json:"body"` // Node, or []Node for a block
}

// --- Expressions ---

type IntLiteral struct {
	Base
	Value int `json:"value"`
}

type FloatLiteral struct {
	Base
	Value float64 `json:"value"`
}

type BoolLiteral struct {
	Base
	Value bool `json:"value"`
}

type StringLiteral struct {
	Base
	Value string `json:"value"` // quotes stripped, escapes kept verbatim
}

type DurationLiteral struct {
	Base
	Microseconds int    `json:"microseconds"`
	Original     string `json:"original"`
}

type ArrayLiteral struct {
	Base
	Elements []Node `json:"elements"`
}

type VarRef struct {
	Base
	Name string `json:"name"`
}

type BinaryExpr struct {
	Base
	Left       Node    `json:"left"`
	Op         string  `json:"op"`
	Right      Node    `json:"right"`
	ResultType *string `json:"result_type"`
}

type UnaryExpr struct {
	Base
	Op      string `json:"op"` // "!", "-", "~"
	Operand Node   `json:"operand"`
}

type DotAccess struct {
	Base
	Obj       Node   `json:"obj"`
	FieldName string `json:"field_name"`
}

type FuncCall struct {
	Base
	Name string `json:"name"`
	Args []Node `json:"args"`
}

type MethodCall struct {
	Base
	Obj    Node   `json:"obj"`
	Method string `json:"method"`
	Args   []Node `json:"args"`
}
