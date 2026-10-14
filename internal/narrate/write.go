package narrate

// WriteOptions shape how a renderer lays the module report out. Each
// format reads the options it has a use for and ignores the rest.
type WriteOptions struct {
	// Width is the terminal's width in columns; 0 means 100.
	Width int
	// Color turns on colour and bars in the terminal.
	Color bool
	// Out is the file a page is written to.
	Out string
	// Open opens the page once it is written.
	Open bool
	// All lists every package, not only the ten costliest.
	All bool
	// Warnings lists the change's warnings by package instead of counting
	// them by rule.
	Warnings bool
	// Exit is the exit code the change report ends with; nil prints none.
	Exit *int
	// ChangeOnly writes the change report alone, as check prints it.
	ChangeOnly bool
	// Formats are the formats the report can be written in, which the
	// terminal report's trailer names; package render fills it.
	Formats []string
}
