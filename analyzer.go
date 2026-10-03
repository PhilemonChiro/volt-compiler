package compiler

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Port of analyzer.py — Pass 4+5: hardware validation, task tiers, yield
// points and lifted variables, ISR safety, try-block rules, shared state,
// WCET estimates, state machine verification and assorted warnings.

var isrUnsafeMethods = set("connect", "disconnect", "scan",
	"send", "send_float", "send_int", "publish", "subscribe",
	"on_receive", "on_connect", "on_disconnect",
	"get", "post",
	"save", "load", "delete",
	"temperature", "humidity", "pressure", "lux",
	"init",
	"clear", "text", "show")

var isrUnsafeFunctions = set("log", "sleep", "deep_sleep", "light_sleep", "run")

// Methods that are safe in an ISR (direct register access).
var isrSafeMethods = set("on", "off", "toggle", "write", "read", "is_on", "is_pressed", "is_connected", "set_pwm")

// Calls that make a task Priority tier (FreeRTOS task).
var blockingAPIs = set(
	"wifi.connect", "wifi.disconnect", "wifi.scan", "wifi.on_connect", "wifi.on_disconnect",
	"mqtt.connect", "mqtt.disconnect", "mqtt.on_receive",
	"mqtt.send", "mqtt.send_int", "mqtt.send_float",
	"http.get", "http.post",
	"storage.save", "storage.load")

// Rough cycle costs for WCET estimation (ESP32 @ 240 MHz).
const (
	wcetGPIO       = 10
	wcetIntegerOp  = 1
	wcetLog        = 5000
	wcetWifi       = 100000
	wcetSensorRead = 10000
	wcetStorage    = 20000
	wcetSleep      = 0
	wcetUnknown    = 100
	isrMaxCycles   = 5000 // ~20 us at 240 MHz
)

func set(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// Analyze runs all analyses on the program.
func Analyze(program *Program, scope *Scope, errs *Errors) {
	// 0. Hardware validation. An unknown chip falls back to the legacy
	// ESP32 table. (Python swallows any exception raised in here.)
	var chip chipSpec
	if program.Chip != nil {
		chip = getChip(program.Chip.Target)
	}
	validateHardware(program, errs, chip)

	// 1. Classify tasks
	blockingDeviceCalls = deviceBlockingCalls(program)
	for _, t := range program.Tasks {
		t.Tier = classifyTask(t)
		t.HasStrict = hasStrictBlock(t.Body)
	}

	// 2. Yield points and crossing variables
	for _, t := range program.Tasks {
		if t.Tier != "background" {
			continue
		}
		if yields := findYieldPoints(t.Body); len(yields) > 0 {
			t.CrossingVars = findCrossingVars(t.Body, yields)
			for _, s := range walkStmts(t.Body) {
				if v, ok := s.(*VarDecl); ok && slices.Contains(t.CrossingVars, v.Name) {
					v.NeedsLifting = true
				}
			}
		}
	}

	// 3. Yielding functions are inlined, so they can't recurse
	for _, f := range program.Functions {
		f.ContainsYield = len(findYieldPoints(f.Body)) > 0
		if f.ContainsYield && callsSelf(f.Body, f.Name) {
			errs.Error(f.Region, "TASK ERROR",
				fmt.Sprintf("Function `%s` contains a yield (sleep/every/on) and calls itself recursively. Recursive yielding functions cannot be inlined into protothreads.", f.Name),
				"Rewrite as a while loop instead of recursion.")
		}
	}

	// 4. ISR safety
	for _, t := range program.Tasks {
		if t.Tier == "critical" {
			checkCriticalSafety(t, errs)
		}
	}
	checkTryBlockRestrictions(program, errs)
	checkDevicePinParams(program, errs)
	detectSharedState(program, errs)

	// 6. WCET for critical tasks
	for _, t := range program.Tasks {
		if t.Tier == "critical" {
			checkCriticalWCET(t, errs)
		}
	}

	// 7. State machines
	for _, m := range program.Machines {
		verifyStateMachine(m, errs)
	}

	// 8. Background tasks with no yield points
	for _, t := range program.Tasks {
		if t.Tier == "background" && len(findYieldPoints(t.Body)) == 0 && len(t.Body) > 0 {
			errs.Warning(t.Region, "TASK WARNING",
				fmt.Sprintf("Task `%s` has no yield points (no every/on/sleep). It will run once and complete.", t.Name), "")
		}
	}

	// 9. Code after every/on in tasks
	for _, t := range program.Tasks {
		warnUnreachableAfterLoop(t.Body, t.Name, errs)
	}

	// 10. Code after run() in setup
	if program.Setup != nil {
		warnCodeAfterRun(program.Setup.Body, errs)
	}

	// 11. break/continue only inside loops
	var bodies [][]Node
	for _, t := range program.Tasks {
		bodies = append(bodies, t.Body)
	}
	for _, f := range program.Functions {
		bodies = append(bodies, f.Body)
	}
	if program.Setup != nil {
		bodies = append(bodies, program.Setup.Body)
	}
	if program.Loop != nil {
		bodies = append(bodies, program.Loop.Body)
	}
	for _, b := range bodies {
		validateBreakContinue(b, false, errs)
	}

	// 12. Non-void functions without a return
	for _, f := range program.Functions {
		if rt := orStr(f.ReturnType, ""); rt != "" && rt != "void" && !hasReturn(f.Body) {
			errs.Warning(f.Region, "MISSING RETURN",
				fmt.Sprintf("Function `%s` declares return type `%s` but may not return a value on all paths.", f.Name, rt), "")
		}
	}

	// 13. Channel capacities
	for _, ch := range program.Channels {
		if ch.Capacity < 1 {
			errs.Error(ch.Region, "CHANNEL ERROR", fmt.Sprintf("Channel `%s` capacity must be at least 1.", ch.Name),
				"Example: `channel data: int[8]`")
		} else if ch.Capacity > 1024 {
			errs.Error(ch.Region, "CHANNEL ERROR",
				fmt.Sprintf("Channel `%s` capacity %d exceeds maximum (1024).", ch.Name, ch.Capacity),
				"Embedded devices have limited RAM. Use 1-1024.")
		}
	}

	// 14. Unused channels
	if len(program.Channels) > 0 {
		warnUnusedChannels(program, errs)
	}

	// loop and tasks can't coexist
	if program.Loop != nil && len(program.Tasks) > 0 {
		errs.Error(program.Loop.Region, "PROGRAM ERROR",
			"Cannot use `loop` and `task` in the same program. Use `loop` for simple programs or `task` for concurrent ones.", "")
	}
}

// ---- 1. Task classification ----

// classifyTask: `every ... strict` → critical (hardware timer ISR); a
// blocking API call → priority (FreeRTOS task); otherwise background
// (protothread).
func classifyTask(t *TaskDecl) string {
	if hasStrictBlock(t.Body) {
		return "critical"
	}
	for _, c := range allCalls(t.Body) {
		name := callFullName(c)
		if blockingAPIs[name] || blockingDeviceCalls[name] {
			return "priority"
		}
	}
	return "background"
}

// blockingDeviceCalls holds "<device>.<method>" for declared devices whose
// device.json marks the method "blocking" (set per Analyze call).
var blockingDeviceCalls = map[string]bool{}

func deviceBlockingCalls(program *Program) map[string]bool {
	out := map[string]bool{}
	if program.Hardware == nil {
		return out
	}
	for _, s := range program.Hardware.Sensors {
		dev := Registry().Get(s.SensorType)
		if dev == nil {
			continue
		}
		for _, m := range dev.Methods {
			if m.Blocking {
				out[s.Name+"."+m.Name] = true
			}
		}
	}
	return out
}

func hasStrictBlock(stmts []Node) bool {
	for _, s := range walkStmts(stmts) {
		if e, ok := s.(*EveryBlock); ok && e.Strict {
			return true
		}
	}
	return false
}

// ---- 2. Yield points ----

func findYieldPoints(stmts []Node) []Region {
	var out []Region
	for _, s := range walkStmts(stmts) {
		switch s := s.(type) {
		case *EveryBlock:
			if !s.Strict {
				out = append(out, s.Region)
			}
		case *OnBlock:
			out = append(out, s.Region)
		case *ExprStmt:
			// direct calls, and calls wrapped in `or` (sleep(1s) or skip)
			call := s.Expr
			if o, ok := call.(*OrExpr); ok {
				call = o.Expr
			}
			if fc, ok := call.(*FuncCall); ok && (fc.Name == "sleep" || fc.Name == "deep_sleep" || fc.Name == "light_sleep") {
				out = append(out, s.Region)
			}
		case *TimeoutBlock:
			out = append(out, s.Region)
		}
	}
	return out
}

// findCrossingVars returns, sorted, the variables declared before a yield
// point and used at or after it (by line).
func findCrossingVars(stmts []Node, yields []Region) []string {
	declLine := map[string]int{}
	var names []string
	for _, s := range walkStmts(stmts) {
		if v, ok := s.(*VarDecl); ok {
			if _, seen := declLine[v.Name]; !seen {
				names = append(names, v.Name)
			}
			declLine[v.Name] = v.Region.StartLine
		}
	}
	lastUse := map[string]int{}
	for _, e := range walkExprs(stmts) {
		if v, ok := e.(*VarRef); ok {
			if _, declared := declLine[v.Name]; declared {
				lastUse[v.Name] = max(lastUse[v.Name], v.Region.StartLine)
			}
		}
	}
	var crossing []string
	for _, name := range names {
		decl := declLine[name]
		use, ok := lastUse[name]
		if !ok {
			use = decl
		}
		for _, y := range yields {
			if decl < y.StartLine && y.StartLine <= use {
				crossing = append(crossing, name)
				break
			}
		}
	}
	sort.Strings(crossing)
	return crossing
}

func hasReturn(stmts []Node) bool {
	for _, s := range stmts {
		switch s := s.(type) {
		case *ReturnStmt:
			return true
		case *IfStmt:
			if hasReturn(s.ThenBody) {
				return true
			}
			switch e := s.ElseBody.(type) {
			case []Node:
				if hasReturn(e) {
					return true
				}
			case *IfStmt:
				if hasReturn([]Node{e}) {
					return true
				}
			}
		case *WhileStmt:
			if hasReturn(s.Body) {
				return true
			}
		case *ForStmt:
			if hasReturn(s.Body) {
				return true
			}
		case *EveryBlock:
			if hasReturn(s.Body) {
				return true
			}
		}
	}
	return false
}

func callsSelf(stmts []Node, name string) bool {
	for _, c := range allCalls(stmts) {
		if fc, ok := c.(*FuncCall); ok && fc.Name == name {
			return true
		}
	}
	return false
}

// ---- 4. ISR safety ----

func checkCriticalSafety(t *TaskDecl, errs *Errors) {
	for _, c := range allCalls(t.Body) {
		switch c := c.(type) {
		case *FuncCall:
			if isrUnsafeFunctions[c.Name] {
				errs.Error(c.Region, "TASK ERROR",
					fmt.Sprintf("Critical task `%s` calls `%s()` which is not safe in ISR context (hardware timer interrupt).", t.Name, c.Name),
					fmt.Sprintf("Remove `%s()` from this task, or remove `strict` to make it a Background task.", c.Name))
			}
		case *MethodCall:
			if isrUnsafeMethods[c.Method] && !isrSafeMethods[c.Method] {
				errs.Error(c.Region, "TASK ERROR",
					fmt.Sprintf("Critical task `%s` calls `%s.%s()` which is not safe in ISR context (may block on I2C/SPI/UART).", t.Name, objName(c.Obj), c.Method),
					"Move this to a Background or Priority task. Share data with the critical task via a volatile variable.")
			}
		}
	}
}

// objName is the receiver's name, or "?" if it isn't a plain name.
func objName(obj Node) string {
	if v, ok := obj.(*VarRef); ok {
		return v.Name
	}
	return "?"
}

// ---- 4c. Device pin-param completeness ----

var placeholderRe = regexp.MustCompile(`\{(\w+)\}`)

var builtinInitKeys = set("pin", "addr", "bus_id", "uart_id")

// checkDevicePinParams: a device whose init_args template names pins
// ({trig}, {echo}, ...) must be declared with the named-pins syntax.
func checkDevicePinParams(program *Program, errs *Errors) {
	if program.Hardware == nil {
		return
	}
	for _, s := range program.Hardware.Sensors {
		dev := Registry().Get(s.SensorType)
		if dev == nil || dev.InitArgs == "" || len(dev.Pins) > 0 {
			continue // declared pins are checked by validateDevices
		}
		var named []string
		for _, m := range placeholderRe.FindAllStringSubmatch(dev.InitArgs, -1) {
			if !builtinInitKeys[m[1]] && dev.Param(m[1]) == nil && !slices.Contains(named, m[1]) {
				named = append(named, m[1])
			}
		}
		if len(named) == 0 {
			continue
		}
		sort.Strings(named)
		var missing []string
		for _, k := range named {
			if _, ok := s.PinParams.Vals[k]; !ok {
				missing = append(missing, k)
			}
		}
		if len(missing) == 0 {
			continue
		}
		params := make([]string, len(named))
		for i, k := range named {
			params[i] = k + ": <gpio>"
		}
		errs.Error(s.Region, "DEVICE PIN ERROR",
			fmt.Sprintf("`%s` needs %d pins (%s) but the declaration only provides one. Missing: %s.",
				s.SensorType, len(named), strings.Join(named, ", "), strings.Join(missing, ", ")),
			fmt.Sprintf("Use the named-pins syntax: `device %s: %s(%s)`", s.Name, s.SensorType, strings.Join(params, ", ")))
	}
}

// ---- 4b. Try-block restrictions ----

// walkTryBlocks lists every TryBlock reachable from stmts. Like Python, a
// try body is walked twice (once as the try's own body, once as a generic
// `body`), so blocks nested in a try appear twice; else-if chains are not
// descended.
func walkTryBlocks(stmts []Node) []*TryBlock {
	var out []*TryBlock
	for _, s := range stmts {
		if s == nil {
			continue
		}
		if tb, ok := s.(*TryBlock); ok {
			out = append(out, tb)
			out = append(out, walkTryBlocks(tb.Body)...)
		}
		for _, inner := range listBodies(s) {
			out = append(out, walkTryBlocks(inner)...)
		}
	}
	return out
}

// listBodies is Python's scan of the body, try_body, fallback_body,
// then_body and (list) else_body attributes, in that order.
func listBodies(s Node) [][]Node {
	switch s := s.(type) {
	case *WhileStmt:
		return [][]Node{s.Body}
	case *ForStmt:
		return [][]Node{s.Body}
	case *EveryBlock:
		return [][]Node{s.Body}
	case *OnBlock:
		return [][]Node{s.Body}
	case *TryBlock:
		return [][]Node{s.Body}
	case *TimeoutBlock:
		return [][]Node{s.TryBody, s.FallbackBody}
	case *IfStmt:
		if e, ok := s.ElseBody.([]Node); ok {
			return [][]Node{s.ThenBody, e}
		}
		return [][]Node{s.ThenBody}
	}
	return nil
}

func checkTryBlockRestrictions(program *Program, errs *Errors) {
	// (a) forbidden in critical tasks
	for _, t := range program.Tasks {
		if t.Tier != "critical" {
			continue
		}
		for _, tb := range walkTryBlocks(t.Body) {
			errs.Error(tb.Region, "TASK ERROR",
				"`try` blocks are forbidden in critical tasks because the body may contain yielding operations.",
				"Move this to a Background or Priority task, or use expression-level `or` clauses at each call site.")
		}
	}

	// (b) unbounded retry in state enter/exit blocks
	for _, m := range program.Machines {
		for _, st := range m.States {
			for _, blk := range []struct {
				name string
				body []Node
			}{{"enter", st.EnterBody}, {"exit", st.ExitBody}} {
				for _, tb := range walkTryBlocks(blk.body) {
					if tb.Policy == "retry" && tb.RetryLimit == nil {
						errs.Error(tb.Region, "STATE MACHINE ERROR",
							fmt.Sprintf("unbounded `or retry` forbidden in state-machine `%s` blocks; would hang the transition.", blk.name),
							"Use bounded retry with an `else` policy: `or retry N times else <skip|restart|fail>`.")
					}
				}
			}
		}
	}

	// (c) mutations in retried bodies, (d) redundant nesting
	var bodies [][]Node
	for _, t := range program.Tasks {
		bodies = append(bodies, t.Body)
	}
	if program.Setup != nil {
		bodies = append(bodies, program.Setup.Body)
	}
	if program.Loop != nil {
		bodies = append(bodies, program.Loop.Body)
	}
	for _, f := range program.Functions {
		bodies = append(bodies, f.Body)
	}
	for _, m := range program.Machines {
		for _, st := range m.States {
			bodies = append(bodies, st.EnterBody, st.ExitBody)
		}
	}
	// Mutating globals/storage or sending on a channel repeats on each retry.
	outer := map[string]bool{}
	for _, v := range program.GlobalVars {
		outer[v.Name] = true
	}
	for _, s := range program.StorageVars {
		outer[s.Name] = true
	}
	channels := map[string]bool{}
	for _, c := range program.Channels {
		channels[c.Name] = true
	}

	for _, body := range bodies {
		for _, tb := range walkTryBlocks(body) {
			if len(tb.Body) == 1 {
				if _, ok := tb.Body[0].(*TryBlock); ok {
					errs.Warning(tb.Region, "REDUNDANT TRY",
						"redundant nested `try` block; merge into outer or remove inner policy.", "")
				}
			}
			if tb.Policy != "retry" {
				continue
			}
			for _, m := range walkMutations(tb.Body, outer, channels) {
				if m.kind == "channel_send" {
					errs.Warning(m.region, "RETRY SIDE EFFECT",
						fmt.Sprintf("channel `.send()` on `%s` inside retry'd `try` block; each retry re-enqueues.", m.name),
						"Move the send outside the try, or accept the duplication.")
				} else {
					errs.Warning(m.region, "RETRY SIDE EFFECT",
						fmt.Sprintf("mutation to `%s` inside retry'd `try` block; side effects will repeat on each retry.", m.name),
						"Move the mutation outside the try, or verify the operation is idempotent.")
				}
			}
		}
	}
}

type mutation struct {
	name, kind string
	region     Region
}

// walkMutations finds assignments to outer names and channel sends, not
// descending into nested try blocks (they're checked on their own).
func walkMutations(stmts []Node, outer, channels map[string]bool) []mutation {
	var out []mutation
	for _, s := range stmts {
		switch s := s.(type) {
		case nil:
			continue
		case *AssignExpr:
			if v, ok := s.Target.(*VarRef); ok && outer[v.Name] {
				out = append(out, mutation{v.Name, "assign", s.Region})
			}
		case *CompoundAssign:
			if v, ok := s.Target.(*VarRef); ok && outer[v.Name] {
				out = append(out, mutation{v.Name, "compound_assign", s.Region})
			}
		case *ExprStmt:
			if mc, ok := s.Expr.(*MethodCall); ok && mc.Method == "send" {
				if v, ok := mc.Obj.(*VarRef); ok && channels[v.Name] {
					out = append(out, mutation{v.Name, "channel_send", s.Region})
				}
			}
		case *TryBlock:
			continue
		}
		for _, inner := range listBodies(s) {
			out = append(out, walkMutations(inner, outer, channels)...)
		}
	}
	return out
}

// ---- 5. Shared state ----

// detectSharedState warns about globals used by several tasks: shared with
// a critical task, or written by more than one task. Codegen makes such
// globals _Atomic, so the warning sits on the variable's declaration.
func detectSharedState(program *Program, errs *Errors) {
	globals := map[string]bool{}
	decl := map[string]Region{}
	for _, v := range program.GlobalVars {
		globals[v.Name] = true
		decl[v.Name] = v.Region
	}
	for _, s := range program.StorageVars {
		globals[s.Name] = true
		decl[s.Name] = s.Region
	}
	// Top-level every/on blocks are auto-named _ruleN; name them by line instead.
	label := map[string]string{}
	for _, t := range program.Tasks {
		label[t.Name] = t.Name
		if strings.HasPrefix(t.Name, "_rule") {
			label[t.Name] = fmt.Sprintf("the block at line %d", t.Region.StartLine)
		}
	}
	where := func(name string) Region {
		if r, ok := decl[name]; ok && r.StartLine > 0 {
			return r
		}
		return program.Region
	}
	type access struct{ task, tier, kind string }
	var names []string
	accesses := map[string][]access{}
	add := func(name string, a access) {
		if _, ok := accesses[name]; !ok {
			names = append(names, name)
		}
		accesses[name] = append(accesses[name], a)
	}
	for _, t := range program.Tasks {
		for _, e := range walkExprs(t.Body) {
			switch e := e.(type) {
			case *VarRef:
				if globals[e.Name] {
					add(e.Name, access{t.Name, t.Tier, "read"})
				}
			case *AssignExpr:
				if v, ok := e.Target.(*VarRef); ok && globals[v.Name] {
					add(v.Name, access{t.Name, t.Tier, "write"})
				}
			}
		}
	}

	for _, name := range names {
		acc := accesses[name]
		tasks, tiers := map[string]bool{}, map[string]bool{}
		var writers []string
		for _, a := range acc {
			tasks[a.task], tiers[a.tier] = true, true
			if a.kind == "write" {
				writers = append(writers, a.task)
			}
		}
		if len(tasks) < 2 {
			continue
		}
		if tiers["critical"] && len(tiers) > 1 {
			var crit, other []string
			for _, a := range acc {
				if a.tier == "critical" {
					crit = append(crit, label[a.task])
				} else {
					other = append(other, label[a.task])
				}
			}
			crit, other = uniqueStrings(crit), uniqueStrings(other)
			errs.Warning(where(name), "CONCURRENCY",
				fmt.Sprintf("Variable `%s` is shared between %s (critical) and %s. Each read and write is atomic in the generated C.",
					name, strings.Join(crit, ", "), strings.Join(other, ", ")),
				"An update spread over several statements can still interleave; keep it in one statement or use a channel.")
		} else if uniq := uniqueStrings(writers); len(writers) > 1 && len(uniq) > 1 {
			for i, w := range uniq {
				uniq[i] = label[w]
			}
			errs.Warning(where(name), "CONCURRENCY WARNING",
				fmt.Sprintf("Variable `%s` is written by %d tasks: %s. Each write is atomic, but the last one wins.", name, len(uniq), strings.Join(uniq, ", ")),
				"Consider using a channel for inter-task communication.")
		}
	}
}

// uniqueStrings drops repeats, keeping first occurrences in order.
func uniqueStrings(xs []string) []string {
	var out []string
	for _, x := range xs {
		if !slices.Contains(out, x) {
			out = append(out, x)
		}
	}
	return out
}

// ---- 6. WCET ----

type costItem struct {
	name string
	cost int
}

func checkCriticalWCET(t *TaskDecl, errs *Errors) {
	cycles, breakdown := estimateWCET(t.Body)
	if cycles <= isrMaxCycles {
		return
	}
	detail := ""
	if len(breakdown) > 0 {
		sort.SliceStable(breakdown, func(i, j int) bool { return breakdown[i].cost > breakdown[j].cost })
		var top []string
		for _, b := range breakdown[:min(3, len(breakdown))] {
			top = append(top, fmt.Sprintf("%s(~%d cycles)", b.name, b.cost))
		}
		detail = " Top costs: " + strings.Join(top, ", ")
	}
	errs.Warning(t.Region, "TASK WARNING",
		fmt.Sprintf("Critical task `%s` estimated at ~%d cycles (~%dus at 240MHz), may exceed ISR time budget.%s",
			t.Name, cycles, pyFloorDiv(cycles*1000, 240000), detail),
		"Keep critical tasks under ~20us. Move heavy work to a Background task.")
}

func pyFloorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func estimateWCET(stmts []Node) (int, []costItem) {
	total := 0
	var breakdown []costItem
	for _, s := range walkStmts(stmts) {
		switch s := s.(type) {
		case *ExprStmt:
			cost, name := exprCost(s.Expr)
			total += cost
			if cost > 10 {
				breakdown = append(breakdown, costItem{name, cost})
			}
		case *VarDecl:
			cost := 1
			if s.Value != nil {
				cost, _ = exprCost(s.Value)
			}
			total += cost + 1 // +1 for the assignment
		case *AssignExpr:
			cost, _ := exprCost(s.Value)
			total += cost + 1
		case *IfStmt, *WhileStmt, *ForStmt:
			total += 5 // branch
		case *ReturnStmt:
			total += 2
		}
	}
	return total, breakdown
}

func exprCost(expr Node) (int, string) {
	switch e := expr.(type) {
	case nil:
		return 0, ""
	case *IntLiteral, *FloatLiteral, *BoolLiteral, *StringLiteral:
		return 1, "literal"
	case *VarRef:
		return 1, "var_read"
	case *BinaryExpr:
		l, _ := exprCost(e.Left)
		r, _ := exprCost(e.Right)
		// Python meant to charge float ops more, but its test never matches.
		return l + r + wcetIntegerOp, "op:" + e.Op
	case *FuncCall:
		switch e.Name {
		case "log":
			return wcetLog, "log()"
		case "sleep", "deep_sleep":
			return wcetSleep, "sleep()"
		}
		return wcetUnknown, e.Name + "()"
	case *MethodCall:
		name := objName(e.Obj) + "." + e.Method + "()"
		switch e.Method {
		case "on", "off", "toggle", "write", "read", "set_pwm":
			return wcetGPIO, name
		case "temperature", "humidity", "pressure", "lux":
			return wcetSensorRead, name
		case "connect", "disconnect", "send", "publish":
			return wcetWifi, name
		case "save", "load":
			return wcetStorage, name
		}
		return wcetUnknown, name
	}
	return wcetUnknown, "unknown"
}

// ---- AST walking helpers ----

// walkStmts lists all statements, recursing into if/while/for/every/on/
// timeout blocks and match arms (not try blocks).
func walkStmts(stmts []Node) []Node {
	var out []Node
	var walk func([]Node)
	walk = func(stmts []Node) {
		for _, s := range stmts {
			if s == nil {
				continue
			}
			out = append(out, s)
			switch s := s.(type) {
			case *IfStmt:
				walk(s.ThenBody)
				switch e := s.ElseBody.(type) {
				case []Node:
					walk(e)
				case *IfStmt:
					walk([]Node{e})
				}
			case *WhileStmt:
				walk(s.Body)
			case *ForStmt:
				walk(s.Body)
			case *EveryBlock:
				walk(s.Body)
			case *OnBlock:
				walk(s.Body)
			case *TimeoutBlock:
				walk(s.TryBody)
				walk(s.FallbackBody)
			case *MatchStmt:
				for _, arm := range s.Arms {
					if b, ok := arm.Body.([]Node); ok {
						walk(b)
					}
				}
			}
		}
	}
	walk(stmts)
	return out
}

// walkExprs lists every expression in the statements (an assignment
// statement is listed itself, before its target and value).
func walkExprs(stmts []Node) []Node {
	var out []Node
	for _, s := range walkStmts(stmts) {
		switch s := s.(type) {
		case *VarDecl:
			out = walkExprTree(s.Value, out)
		case *AssignExpr:
			out = append(out, s)
			out = walkExprTree(s.Target, out)
			out = walkExprTree(s.Value, out)
		case *CompoundAssign:
			out = walkExprTree(s.Target, out)
			out = walkExprTree(s.Value, out)
		case *ExprStmt:
			out = walkExprTree(s.Expr, out)
		case *ReturnStmt:
			out = walkExprTree(s.Value, out)
		case *IfStmt:
			out = walkExprTree(s.Condition, out)
		case *WhileStmt:
			out = walkExprTree(s.Condition, out)
		case *EveryBlock:
			out = walkExprTree(s.Interval, out)
		case *OnBlock:
			out = walkExprTree(s.Event, out)
		}
	}
	return out
}

// walkExprTree appends e and its sub-expressions (not through `or`).
func walkExprTree(e Node, out []Node) []Node {
	if e == nil {
		return out
	}
	out = append(out, e)
	switch e := e.(type) {
	case *BinaryExpr:
		out = walkExprTree(e.Left, out)
		out = walkExprTree(e.Right, out)
	case *UnaryExpr:
		out = walkExprTree(e.Operand, out)
	case *FuncCall:
		for _, a := range e.Args {
			out = walkExprTree(a, out)
		}
	case *MethodCall:
		out = walkExprTree(e.Obj, out)
		for _, a := range e.Args {
			out = walkExprTree(a, out)
		}
	case *AssignExpr:
		out = walkExprTree(e.Target, out)
		out = walkExprTree(e.Value, out)
	case *DotAccess:
		out = walkExprTree(e.Obj, out)
	case *ArrayLiteral:
		for _, el := range e.Elements {
			out = walkExprTree(el, out)
		}
	}
	return out
}

func allCalls(stmts []Node) []Node {
	var out []Node
	for _, e := range walkExprs(stmts) {
		switch e.(type) {
		case *FuncCall, *MethodCall:
			out = append(out, e)
		}
	}
	return out
}

// callFullName: "log", "wifi.connect", "?.method".
func callFullName(c Node) string {
	switch c := c.(type) {
	case *FuncCall:
		return c.Name
	case *MethodCall:
		return objName(c.Obj) + "." + c.Method
	}
	return ""
}

// ---- 7. State machines ----

func verifyStateMachine(m *MachineDecl, errs *Errors) {
	if len(m.States) == 0 {
		errs.Error(m.Region, "STATE MACHINE ERROR", fmt.Sprintf("Machine `%s` has no states.", m.Name), "")
		return
	}
	var names []string // unique, in declaration order
	byName := map[string]*StateDecl{}
	for _, s := range m.States {
		if _, ok := byName[s.Name]; !ok {
			names = append(names, s.Name)
			byName[s.Name] = s
		}
	}
	sorted := slices.Sorted(slices.Values(names))

	var initial []*StateDecl
	for _, s := range m.States {
		if s.IsInitial {
			initial = append(initial, s)
		}
	}
	switch {
	case len(initial) == 0: // the first state is initial by default
		m.States[0].IsInitial = true
		initial = m.States[:1]
	case len(initial) > 1:
		ns := make([]string, len(initial))
		for i, s := range initial {
			ns[i] = s.Name
		}
		errs.Error(m.Region, "STATE MACHINE ERROR",
			fmt.Sprintf("Machine `%s` has %d initial states (%s). Only one allowed.", m.Name, len(initial), strings.Join(ns, ", ")), "")
		return
	}
	start := initial[0]

	for _, s := range m.States {
		for _, tr := range s.Transitions {
			if byName[tr.TargetState] == nil {
				errs.Error(tr.Region, "STATE MACHINE ERROR",
					fmt.Sprintf("Transition target `%s` does not exist. Available states: %s.", tr.TargetState, strings.Join(sorted, ", ")), "")
			}
		}
		for _, tt := range s.TimedTransitions {
			if byName[tt.TargetState] == nil {
				errs.Error(tt.Region, "STATE MACHINE ERROR",
					fmt.Sprintf("Timed transition target `%s` does not exist. Available states: %s.", tt.TargetState, strings.Join(sorted, ", ")), "")
			}
		}
	}

	// Reachability (BFS from the initial state)
	reachable := map[string]bool{}
	queue := []string{start.Name}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if reachable[cur] {
			continue
		}
		reachable[cur] = true
		st := byName[cur]
		if st == nil {
			continue
		}
		for _, tr := range st.Transitions {
			if !reachable[tr.TargetState] {
				queue = append(queue, tr.TargetState)
			}
		}
		for _, tt := range st.TimedTransitions {
			if !reachable[tt.TargetState] {
				queue = append(queue, tt.TargetState)
			}
		}
	}
	// Python iterates a set here (arbitrary order); declaration order is
	// the deterministic choice.
	for _, n := range names {
		if !reachable[n] {
			errs.Warning(byName[n].Region, "STATE MACHINE WARNING",
				fmt.Sprintf("State `%s` in machine `%s` is unreachable from initial state `%s`.", n, m.Name, start.Name), "")
		}
	}

	for _, s := range m.States {
		if len(s.Transitions) == 0 && len(s.TimedTransitions) == 0 {
			errs.Warning(s.Region, "STATE MACHINE WARNING",
				fmt.Sprintf("State `%s` in machine `%s` has no outgoing transitions (dead-end state).", s.Name, m.Name),
				"Add a transition or mark with @terminal if intentional.")
		}
	}

	// Determinism: transitions on the same event need guards.
	for _, s := range m.States {
		var keys []string
		groups := map[string][]*StateTransition{}
		for _, tr := range s.Transitions {
			k := eventKey(tr.Event)
			if _, ok := groups[k]; !ok {
				keys = append(keys, k)
			}
			groups[k] = append(groups[k], tr)
		}
		for _, k := range keys {
			trs := groups[k]
			if len(trs) <= 1 {
				continue
			}
			unguarded := 0
			for _, tr := range trs {
				if tr.Guard == nil {
					unguarded++
				}
			}
			if unguarded > 1 {
				errs.Error(s.Region, "STATE MACHINE ERROR",
					fmt.Sprintf("State `%s`: multiple unguarded transitions for event `%s`. Add guards [condition] to distinguish them.", s.Name, k), "")
			} else if unguarded == 0 {
				errs.Warning(s.Region, "STATE MACHINE WARNING",
					fmt.Sprintf("State `%s`: all transitions for event `%s` have guards. If no guard matches, event is silently dropped.", s.Name, k),
					"Add a default (unguarded) transition as fallback.")
			}
		}
	}

	// `or skip` needs a loop to continue; state blocks aren't loops.
	for _, s := range m.States {
		checkNoOrSkip(s.EnterBody, fmt.Sprintf("enter block of state `%s`", s.Name), errs)
		checkNoOrSkip(s.ExitBody, fmt.Sprintf("exit block of state `%s`", s.Name), errs)
		for _, tr := range s.Transitions {
			checkNoOrSkip(tr.Action, fmt.Sprintf("transition action in state `%s`", s.Name), errs)
		}
	}
}

func checkNoOrSkip(stmts []Node, context string, errs *Errors) {
	for _, s := range stmts {
		var expr Node
		switch s := s.(type) {
		case *ExprStmt:
			expr = s.Expr
		case *VarDecl:
			expr = s.Value
		}
		if o, ok := expr.(*OrExpr); ok && o.Kind == "skip" {
			errs.Error(regionOf(s), "STATE MACHINE ERROR",
				fmt.Sprintf("`or skip` cannot be used in %s — state blocks are not loops.", context),
				"Use `or restart` or `or <default value>` instead.")
		}
		if ifs, ok := s.(*IfStmt); ok {
			checkNoOrSkip(ifs.ThenBody, context, errs)
			switch e := ifs.ElseBody.(type) {
			case []Node:
				checkNoOrSkip(e, context, errs)
			case *IfStmt:
				checkNoOrSkip([]Node{e}, context, errs)
			}
		}
	}
}

// eventKey groups transitions by event; other expressions use Python's repr.
func eventKey(ev Node) string {
	switch e := ev.(type) {
	case *MethodCall:
		return objName(e.Obj) + "." + e.Method
	case *FuncCall:
		return e.Name
	case *VarRef:
		return e.Name
	}
	return pyRepr(reflect.ValueOf(ev))
}

// ---- 9-10. Unreachable code ----

func warnUnreachableAfterLoop(stmts []Node, task string, errs *Errors) {
	found := false
	for _, s := range stmts {
		if found {
			errs.Warning(regionOf(s), "UNREACHABLE",
				fmt.Sprintf("Code after `every`/`on` block in task `%s` will never execute.", task),
				"Move this code before the `every` block, or into a separate task.")
			return
		}
		switch s := s.(type) {
		case *EveryBlock:
			found = !s.Strict
		case *OnBlock:
			found = true
		}
	}
}

func warnCodeAfterRun(stmts []Node, errs *Errors) {
	found := false
	for _, s := range stmts {
		if found {
			errs.Warning(regionOf(s), "UNREACHABLE",
				"Code after `run()` will never execute. `run()` starts the task scheduler and blocks forever.",
				"Move this code before `run()`.")
			return
		}
		if es, ok := s.(*ExprStmt); ok {
			if fc, ok := es.Expr.(*FuncCall); ok && fc.Name == "run" {
				found = true
			}
		}
	}
}

// validateBreakContinue: break/continue only inside while/for/every/on.
func validateBreakContinue(stmts []Node, inLoop bool, errs *Errors) {
	for _, s := range stmts {
		switch s := s.(type) {
		case *BreakStmt:
			if !inLoop {
				errs.Error(s.Region, "SYNTAX ERROR", "`break` can only be used inside a loop (while, for, loop).", "Move this into a loop block.")
			}
		case *ContinueStmt:
			if !inLoop {
				errs.Error(s.Region, "SYNTAX ERROR", "`continue` can only be used inside a loop (while, for, loop).", "Move this into a loop block.")
			}
		case *WhileStmt:
			validateBreakContinue(s.Body, true, errs)
		case *ForStmt:
			validateBreakContinue(s.Body, true, errs)
		case *IfStmt:
			validateBreakContinue(s.ThenBody, inLoop, errs)
			switch e := s.ElseBody.(type) {
			case []Node:
				validateBreakContinue(e, inLoop, errs)
			case *IfStmt:
				validateBreakContinue([]Node{e}, inLoop, errs)
			}
		case *EveryBlock:
			validateBreakContinue(s.Body, true, errs)
		case *OnBlock:
			validateBreakContinue(s.Body, true, errs)
		case *TimeoutBlock:
			validateBreakContinue(s.TryBody, inLoop, errs)
			validateBreakContinue(s.FallbackBody, inLoop, errs)
		}
	}
}

// ---- 14. Unused channels ----

func warnUnusedChannels(program *Program, errs *Errors) {
	chans := map[string]bool{}
	for _, c := range program.Channels {
		chans[c.Name] = true
	}
	consumed, sent := map[string]bool{}, map[string]bool{}

	var scanExpr func(Node)
	scanExpr = func(e Node) {
		switch e := e.(type) {
		case *MethodCall:
			scanExpr(e.Obj)
			if v, ok := e.Obj.(*VarRef); ok && chans[v.Name] {
				switch e.Method {
				case "receive", "try_receive", "has_data":
					consumed[v.Name] = true
				case "send":
					sent[v.Name] = true
				}
			}
			for _, a := range e.Args {
				scanExpr(a)
			}
		case *FuncCall:
			for _, a := range e.Args {
				scanExpr(a)
			}
		case *BinaryExpr:
			scanExpr(e.Left)
			scanExpr(e.Right)
		case *UnaryExpr:
			scanExpr(e.Operand)
		}
	}
	var scanStmts func([]Node)
	scanStmts = func(stmts []Node) {
		for _, s := range stmts {
			switch s := s.(type) {
			case *ExprStmt:
				scanExpr(s.Expr)
			case *VarDecl:
				scanExpr(s.Value)
			case *AssignExpr:
				scanExpr(s.Target)
				scanExpr(s.Value)
			case *CompoundAssign:
				scanExpr(s.Target)
				scanExpr(s.Value)
			case *IfStmt:
				scanExpr(s.Condition)
				scanStmts(s.ThenBody)
				switch e := s.ElseBody.(type) {
				case []Node:
					scanStmts(e)
				case *IfStmt:
					scanStmts([]Node{e})
				}
			case *WhileStmt:
				scanExpr(s.Condition)
				scanStmts(s.Body)
			case *ForStmt:
				scanExpr(s.Start)
				scanExpr(s.End)
				scanStmts(s.Body)
			case *EveryBlock:
				scanStmts(s.Body)
			case *OnBlock:
				scanExpr(s.Event)
				scanStmts(s.Body)
			case *ReturnStmt:
				scanExpr(s.Value)
			}
		}
	}
	for _, t := range program.Tasks {
		scanStmts(t.Body)
	}
	if program.Setup != nil {
		scanStmts(program.Setup.Body)
	}
	if program.Loop != nil {
		scanStmts(program.Loop.Body)
	}

	for _, c := range program.Channels {
		switch {
		case sent[c.Name] && !consumed[c.Name]:
			errs.Warning(c.Region, "CHANNEL WARNING",
				fmt.Sprintf("Channel `%s` is sent to but never consumed (no receive/has_data call).", c.Name),
				fmt.Sprintf("Add `%s.receive()` or `on %s.has_data()` in a task to process the data.", c.Name, c.Name))
		case !sent[c.Name] && !consumed[c.Name]:
			errs.Warning(c.Region, "CHANNEL WARNING", fmt.Sprintf("Channel `%s` is declared but never used.", c.Name), "")
		}
	}
}
