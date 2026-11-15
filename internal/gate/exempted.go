package gate

// Exempted is a violation, or a warn rule's breach, that an exemption
// silenced (package exempt), with the exemption's reason.
type Exempted struct {
	Violation
	// Reason is the matching exemption's reason.
	Reason string
}
