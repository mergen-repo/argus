package dsl

type Node interface {
	nodeType() string
}

type Statement interface {
	Node
	statementNode()
}

type WhenBody interface {
	Node
	whenBodyNode()
}

type Condition interface {
	Node
	conditionNode()
}

type Value interface {
	Node
	valueType() string
}

type Policy struct {
	Name     string
	Match    *MatchBlock
	Rules    *RulesBlock
	Charging *ChargingBlock
}

func (p *Policy) nodeType() string { return "Policy" }

type MatchBlock struct {
	Clauses []*MatchClause
}

func (m *MatchBlock) nodeType() string { return "MatchBlock" }

type MatchClause struct {
	Field    string
	Operator string
	Values   []Value
	Line     int
	Column   int
}

func (m *MatchClause) nodeType() string { return "MatchClause" }

type RulesBlock struct {
	Statements []Statement
}

func (r *RulesBlock) nodeType() string { return "RulesBlock" }

type Assignment struct {
	Property string
	Val      Value
	Line     int
	Column   int
}

func (a *Assignment) nodeType() string    { return "Assignment" }
func (a *Assignment) statementNode()      {}
func (a *Assignment) whenBodyNode()       {}
func (a *Assignment) chargingStmtNode()   {}

type WhenBlock struct {
	Cond Condition
	Body []WhenBody
	Line int
}

func (w *WhenBlock) nodeType() string { return "WhenBlock" }
func (w *WhenBlock) statementNode()   {}

type ActionCall struct {
	Name string
	Args []*Argument
	Line int
}

func (a *ActionCall) nodeType() string { return "ActionCall" }
func (a *ActionCall) whenBodyNode()    {}

type Argument struct {
	Name string
	Val  Value
}

func (a *Argument) nodeType() string { return "Argument" }

type SimpleCondition struct {
	Field    string
	Operator string
	Values   []Value
	Line     int
	Column   int
}

func (s *SimpleCondition) nodeType() string  { return "SimpleCondition" }
func (s *SimpleCondition) conditionNode()    {}

type CompoundCondition struct {
	Left  Condition
	Op    string
	Right Condition
}

func (c *CompoundCondition) nodeType() string  { return "CompoundCondition" }
func (c *CompoundCondition) conditionNode()    {}

type NotCondition struct {
	Inner Condition
}

func (n *NotCondition) nodeType() string  { return "NotCondition" }
func (n *NotCondition) conditionNode()    {}

type GroupCondition struct {
	Inner Condition
}

func (g *GroupCondition) nodeType() string  { return "GroupCondition" }
func (g *GroupCondition) conditionNode()    {}

type ChargingBlock struct {
	Assignments   []*Assignment
	RATMultiplier map[string]float64
}

func (c *ChargingBlock) nodeType() string { return "ChargingBlock" }

type StringValue struct {
	Val    string
	Line   int
	Column int
}

func (s *StringValue) nodeType() string  { return "StringValue" }
func (s *StringValue) valueType() string { return "string" }

type NumberValue struct {
	Val    float64
	Line   int
	Column int
}

func (n *NumberValue) nodeType() string  { return "NumberValue" }
func (n *NumberValue) valueType() string { return "number" }

type NumberWithUnit struct {
	Val    float64
	Unit   string
	Line   int
	Column int
}

func (n *NumberWithUnit) nodeType() string  { return "NumberWithUnit" }
func (n *NumberWithUnit) valueType() string { return "number_with_unit" }

type IdentValue struct {
	Val    string
	Line   int
	Column int
}

func (i *IdentValue) nodeType() string  { return "IdentValue" }
func (i *IdentValue) valueType() string { return "ident" }

type TimeRange struct {
	Start  string
	End    string
	Line   int
	Column int
}

func (t *TimeRange) nodeType() string  { return "TimeRange" }
func (t *TimeRange) valueType() string { return "time_range" }

type PercentValue struct {
	Val    float64
	Line   int
	Column int
}

func (p *PercentValue) nodeType() string  { return "PercentValue" }
func (p *PercentValue) valueType() string { return "percent" }

type BoolValue struct {
	Val    bool
	Line   int
	Column int
}

func (b *BoolValue) nodeType() string  { return "BoolValue" }
func (b *BoolValue) valueType() string { return "bool" }
