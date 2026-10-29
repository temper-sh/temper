# Terminal controls

The setup wizard (`temper init` / `temper configure`), preset editor
(`temper preset`) and layout editor (`temper layout`) share keyboard navigation.
You can reach their controls with arrows and Enter; Tab is optional.

Page explanations use a shared header block above primary navigation. Each page
supplies its own title and guidance; selectable content stays below navigation.
All three tools omit this optional guidance in compact terminals to preserve
room for controls and content. Field-specific help and confirmation consequences
remain with their controls. Layout composition explains inclusion, loading on
activation and the default model alias above its navigation.

| Key | Behavior |
|---|---|
| Up / Down | Move through content rows, then into navigation or footer actions at the edge. |
| Left / Right | Choose within the focused navigation, action row or inline controls. Actions and secondary choices wrap; primary steps and breadcrumbs stop at either end. |
| Enter / Space | Activate the focused control. Read-only content has no action to activate. |
| Tab / Shift-Tab | Move directly between regions in either direction. |
| PgUp / PgDown | Scroll content. |
| Escape | Cancel an input or return through the tool's existing back/confirmation flow. |

Focus follows the screen: primary navigation, secondary navigation, content,
then actions. Moving beyond the last region returns to the first. Empty lists
and absent navigation rows are skipped. Down enters the first content row;
Up from the footer enters the last. Read-only pages scroll to their edge before
focus leaves, and returning from the footer preserves their scroll position.
Left/Right selects a row's inline controls when present, such as the layout's
Included / Load on activation / Default checkboxes. In other content lists it
also switches visible sections or categories. Up reaches secondary navigation
from the first row.

Entering the footer focuses the main available action. When it is unavailable,
focus falls back to a safe action; the wizard uses Back while installation is
unavailable. Moving between focus regions never activates a control. Left/Right within a
choice changes its draft value. Disabled controls cannot perform
their action, and Save or installation still requires explicit activation.

Ordinary preset and layout settings are always-visible draft controls. Typing
edits the focused input; Up/Down moves between controls, Left/Right moves a text
cursor or chooses an option, and Enter advances a text input or activates the
focused control. Text input consumes ordinary characters, including shortcut
letters and Space. Tab still moves between regions. Invalid text remains visible
beside its error across navigation and resize, and cannot be saved as an older
valid value. Fixed choices and toggles change only the unsaved draft. Named
modes expose output limits, reasoning budgets, random/fixed seeds and optional
engine settings without asking users to enter sentinel numbers.

Save is explicit; Discard follows the existing guarded exit flow. The wizard’s
Context action or `c`, and Compute action or `p` for included presets with both
CPU/GPU choices, open separate prompts. Compute starts at the current device;
Left/Right, Space or a click changes only its pending choice. Apply updates the
draft and memory assessment, while Cancel or Escape preserves the previous value. Navigation
stays within that prompt until it is applied or cancelled. New-preset and
new-layout naming prompts also retain Cancel / Apply. Template and weights
selection have dedicated source pages, with Cancel
and one completion action. The file list also offers Back to the source.
Cancelling a source page preserves the previous draft and attachment. Local
template browsing only reads files; Use template attaches their bytes to the
draft, and Save publishes the managed copy together with the preset reference.

Confirmations keep focus inside their choices and actions, retaining their safe
initial choice. Pending operations keep their existing cancellation and save
guards. Mouse activation uses the same guarded operations as the keyboard.

[Setup controls and shortcuts](init.md#keyboard-controls) ·
[Preset authoring](user-presets.md#interactive-authoring) ·
[Layout editing](layouts.md#standalone-layout-editor)
