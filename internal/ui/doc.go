// Package ui is the full-screen interface of tote and the only package that imports Charm.
//
// The rest of the program sees a narrow API: Run takes Options and returns a Result that tells
// the shell wrapper what to do afterwards (change directory, insert a command line, reload the
// definitions), ErrInterrupted reports a SIGINT (the Result that comes with it can still ask for
// a reload), and ResetTerminal puts a terminal back in a usable state. A few functions describe
// the terminal for the doctor command without drawing anything: DescribeTerminal, LayoutName,
// MouseState and UnknownColor. No Charm type crosses that boundary; colorprofile, also a Charm
// module, is imported here and nowhere else.
//
// Inside, a single root model owns the shared environment, one screen (the welcome menu, the list
// of a kind or the settings), a stack of overlays (the add and edit form, the confirmations and
// questions, the pickers, the help, the details and the Ctrl-K palette), a toast, a status line
// with the git branch and the result. The mouse (clicks acted on at the release of the button,
// wheel, drag to select and copy) is read from the same layout that draws the frame. Screens and
// overlays are small interfaces, the key bindings they return generate the footer, and every
// user-visible string comes from the catalogs with the ui_ prefix. Records are written by the
// event loop itself, with a lock wait of 150 ms so that a busy file shows a message instead of
// freezing the screen. Text that comes from the data files goes through sanitize before it is
// drawn or measured, colors live in theme.go only, and a program started for the user, such as
// ssh, runs through a custom tea.ExecCommand with its own signal guard.
package ui
