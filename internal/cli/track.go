package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"ai-resume-tailor/internal/store"
)

func runTrack(args []string) error {
	if len(args) < 2 {
		return usagef(`usage: ai-resume-tailor track "<company>" "<role>"`)
	}
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	app, err := st.Add(args[0], args[1])
	if err != nil {
		return err
	}
	fmt.Printf("Tracking application #%d: %s — %s [%s]\n",
		app.ID, app.Company, app.Role, app.Status)
	return nil
}

func runApps() error {
	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	apps, err := st.List()
	if err != nil {
		return err
	}
	if len(apps) == 0 {
		fmt.Println(`No applications tracked yet. Add one with:  ai-resume-tailor track "Company" "Role"`)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tCOMPANY\tROLE\tSTATUS\tAPPLIED\tUPDATED")
	for _, a := range apps {
		applied := "-"
		if a.AppliedAt != nil {
			applied = a.AppliedAt.Local().Format("2006-01-02")
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
			a.ID, a.Company, a.Role, a.Status, applied,
			a.UpdatedAt.Local().Format("2006-01-02"))
	}
	return w.Flush()
}

func runStatus(args []string) error {
	if len(args) < 2 {
		return usagef("usage: ai-resume-tailor status <id> <status>  (statuses: %s)", statusList())
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return usagef("invalid id: %q", args[0])
	}

	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.SetStatus(id, store.Status(args[1])); err != nil {
		return err
	}
	fmt.Printf("Application #%d -> %s\n", id, args[1])
	return nil
}

func runNote(args []string) error {
	if len(args) < 2 {
		return usagef("usage: ai-resume-tailor note <id> <text...>")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return usagef("invalid id: %q", args[0])
	}

	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.SetNotes(id, strings.Join(args[1:], " ")); err != nil {
		return err
	}
	fmt.Printf("Note saved on application #%d\n", id)
	return nil
}

func statusList() string {
	all := store.AllStatuses()
	parts := make([]string, len(all))
	for i, s := range all {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}
