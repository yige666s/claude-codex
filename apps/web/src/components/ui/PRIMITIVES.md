# UI Primitive Usage

These primitives should stay close to shadcn/Radix APIs so feature code can move
from hand-built interactions to consistent, accessible building blocks.

- Use `Button` for commands and wrap icon-only buttons with `Tooltip`.
- Use `Popover` for small anchored controls that need focus management, such as volume sliders.
- Use `Slider` for numeric continuous controls; prefer vertical orientation only when the control is spatially constrained.
- Use `DropdownMenu` for command menus and account/action menus. Keep item labels action-oriented and avoid nesting unless the menu is already crowded.
- Use motion wrappers from `components/motion` for panels, popovers, list items, and message entry animations. Respect reduced-motion behavior through the shared motion config.

Do not introduce page-specific styling in primitive files. Add component variants
only when at least two features can reuse them.
