package tracer

// Breakpoints manages the breakpoints. The breakpoint can be conditional, which means the breakpoint is considered as hit
// only when the specific conditions are met.
type Breakpoints struct {
	setBreakpoints map[uint64]*conditionalBreakpoint
	doSet          func(addr uint64) error
	doClear        func(addr uint64) error
}

// NewBreakpoints returns new Breakpoints. Pass the functions to actually set and clear breakpoints.
func NewBreakpoints(setBreakpiont, clearBreakpiont func(addr uint64) error) Breakpoints {
	return Breakpoints{setBreakpoints: make(map[uint64]*conditionalBreakpoint), doSet: setBreakpiont, doClear: clearBreakpiont}
}

// Hit returns true if the breakpoint is not conditional or the condtional breakpoint meets its condition.
func (b Breakpoints) Hit(addr uint64, goRoutineID int64) bool {
	bp, ok := b.setBreakpoints[addr]
	return ok && bp.Hit(goRoutineID)
}

// Exist returns true if the breakpoint exists.
func (b Breakpoints) Exist(addr uint64) bool {
	_, ok := b.setBreakpoints[addr]
	return ok
}

// Clear clears the breakpoint at the specified address. Conditonal breakpoints for the same address are also cleared.
func (b Breakpoints) Clear(addr uint64) error {
	_, ok := b.setBreakpoints[addr]
	if !ok {
		return nil
	}

	if err := b.doClear(addr); err != nil {
		return err
	}

	delete(b.setBreakpoints, addr)
	return nil
}

// ClearConditional clears the conditional breakpoint for the specified address and go routine.
// The physical breakpoint for the specified address may still exist if other conditional breakpoints specify
// to that address.
func (b Breakpoints) ClearConditional(addr uint64, goRoutineID int64) error {
	bp, ok := b.setBreakpoints[addr]
	if !ok {
		return nil
	}
	bp.Disassociate(goRoutineID)

	if !bp.NoAssociation() {
		return nil
	}

	return b.Clear(addr)
}

// ClearAllByGoRoutineID clears all the breakpoints associated with the specified go routine.
func (b Breakpoints) ClearAllByGoRoutineID(goRoutineID int64) error {
	for addr, bp := range b.setBreakpoints {
		bp.Disassociate(goRoutineID)

		if !bp.NoAssociation() {
			continue
		}
		if err := b.Clear(addr); err != nil {
			return err
		}
	}

	return nil
}

// Set sets the breakpoint at the specified address.
// If `SetConditional` is called before for the same address, the conditions are removed.
func (b Breakpoints) Set(addr uint64) error {
	_, ok := b.setBreakpoints[addr]
	if !ok {
		if err := b.doSet(addr); err != nil {
			return err
		}
	}

	b.setBreakpoints[addr] = &conditionalBreakpoint{addr: addr}
	return nil
}

// SetConditional sets the conditional breakpoint which only the specified go routine is considered as hit.
// If `Set` is called before for the same address, this function is no-op.
func (b Breakpoints) SetConditional(addr uint64, goRoutineID int64) error {
	bp, ok := b.setBreakpoints[addr]
	if ok {
		if !bp.NoAssociation() {
			bp.Associate(goRoutineID)
		}
		return nil
	}

	if err := b.doSet(addr); err != nil {
		return err
	}

	bp = &conditionalBreakpoint{addr: addr}
	bp.Associate(goRoutineID)
	b.setBreakpoints[addr] = bp
	return nil
}

type association struct {
	goRoutineID int64
}

// conditionalBreakpoint is the virtual breakpoint which holds some conditions to be considered as 'hit'
type conditionalBreakpoint struct {
	addr         uint64
	associations []int64
}

// Hit returns true if the specified go routine id is associated.
func (b *conditionalBreakpoint) Hit(goRoutineID int64) bool {
	for _, association := range b.associations {
		if association == goRoutineID {
			return true
		}
	}

	return len(b.associations) == 0
}

// NoAssociation returns true if the breakpoint has no associations.
func (b *conditionalBreakpoint) NoAssociation() bool {
	return len(b.associations) == 0
}

// Associate associates the specified go routine.
func (b *conditionalBreakpoint) Associate(goRoutineID int64) {
	b.associations = append(b.associations, goRoutineID)
	return
}

// Disassociate disassociates the specified go routine.
func (b *conditionalBreakpoint) Disassociate(goRoutineID int64) {
	for i, association := range b.associations {
		if association == goRoutineID {
			b.associations = append(b.associations[0:i], b.associations[i+1:len(b.associations)]...)
			return
		}
	}
	return
}
