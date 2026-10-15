package narrate

// HumanBasis says, in full, where the engineer time comes from. The
// glossary's "Rebuild cost" entry and the JSON report's human_basis field
// carry it. The numbers match the shipped rebuild parameters; they are
// written here rather than read from the configuration because the module
// report is composed without it.
const HumanBasis = "Engineer time is a textbook figure, not a measurement: COCOMO's organic-mode rule of 2.4 person-months per thousand lines (to the power 1.05), 19 working days a month, inflated by up to half where exports have no tests, summed over the packages. The agent sittings were measured on 85 rebuilds. rebuild.cocomo_a in the configuration scales the engineer time."

// HumanBasisShort is HumanBasis in one line, for the footnotes of the text
// and Markdown reports and the hero's tooltip.
const HumanBasisShort = "Engineer time is COCOMO's textbook rule (2.4 person-months per 1,000 lines), not a measurement; agent sittings are measured. rebuild.cocomo_a scales it."
