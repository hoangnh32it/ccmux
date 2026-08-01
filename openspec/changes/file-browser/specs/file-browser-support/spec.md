## ADDED Requirements

### Requirement: Whole-Project File Tree

ccmux SHALL provide a Files screen that lists every file in a
project, not limited to markdown, pruning version-control,
dependency, and build-output directories.

#### Scenario: Every file type is listed

- **GIVEN** a project containing `.go`, `.rs`, `.json`, and `.md`
  files
- **WHEN** the user opens the Files screen for that project
- **THEN** all four files appear in the tree

#### Scenario: Noise directories are pruned

- **GIVEN** a project with a `.git` directory and a `node_modules`
  directory
- **WHEN** the Files screen lists the project tree
- **THEN** neither directory's contents appear in the tree

### Requirement: Syntax-Highlighted File Preview

ccmux SHALL render a syntax-highlighted preview of the selected file
when its language is recognized, and a plain-text preview otherwise.

#### Scenario: Recognized language is highlighted

- **GIVEN** the user selects a `.go` file in the Files screen
- **WHEN** the preview pane renders its contents
- **THEN** the content is syntax-highlighted for Go

#### Scenario: Unrecognized extension falls back to plain text

- **GIVEN** the user selects a file with an extension chroma does not
  recognize
- **WHEN** the preview pane renders its contents
- **THEN** the content is shown as plain, unhighlighted text

#### Scenario: Binary files are not rendered as text

- **GIVEN** the user selects a file detected as binary
- **WHEN** the preview pane would render it
- **THEN** ccmux shows a "binary file" placeholder instead of file
  contents

### Requirement: File Tree Follows Attached Pane's Working Directory

ccmux SHALL keep the Files screen's tree root synchronized with the
current working directory of the tmux pane the user is attached to.

#### Scenario: Tree re-roots after a pane cd

- **GIVEN** the user is attached to a session whose pane changes
  directory
- **WHEN** ccmux next reads that pane's current path
- **THEN** the Files screen's tree root updates to the new directory

### Requirement: Files Screen Mouse Interaction

ccmux SHALL support mouse click-to-focus between the Files screen's
tree and preview panes, and drag-to-resize the split between them.

#### Scenario: Click focuses a pane

- **GIVEN** the Files screen is open with the tree pane focused
- **WHEN** the user clicks inside the preview pane
- **THEN** focus moves to the preview pane

#### Scenario: Dragging the border resizes the split

- **GIVEN** the Files screen is open
- **WHEN** the user drags the border between tree and preview
- **THEN** the width ratio between the two panes updates to follow
  the drag

### Requirement: Files CLI Parity

ccmux SHALL expose the Files screen's core operations as CLI
subcommands, mirroring the existing `notes` command shape.

#### Scenario: List files via CLI

- **WHEN** a user runs `ccmux files list <project>`
- **THEN** ccmux prints every file in that project's tree, respecting
  the same pruning rules as the TUI Files screen

#### Scenario: Read a file via CLI

- **WHEN** a user runs `ccmux files read <project> <path>`
- **THEN** ccmux prints that file's contents

#### Scenario: List/read a remote host's files

- **GIVEN** `--host <name>` refers to a configured, reachable `ccmuxd`
  peer
- **WHEN** a user runs `ccmux files list <project> --host <name>` or
  `ccmux files read <project> <path> --host <name>`
- **THEN** ccmux lists or reads files from that remote host's project
  instead of the local one
