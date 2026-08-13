package cli

import (
	"fmt"
	"log/slog"
	"net/http"

	"ai-resume-tailor/internal/web"
)

func runServe(log *slog.Logger, args []string) error {
	// Bind to localhost only. The tracker holds personal application data, so
	// it should never be exposed on the network by default.
	addr := "127.0.0.1:8080"
	if len(args) >= 1 {
		addr = args[0]
	}

	st, err := openStore()
	if err != nil {
		return err
	}
	defer st.Close()

	srv, err := web.NewServer(st, log)
	if err != nil {
		return err
	}

	fmt.Printf("ai-resume-tailor dashboard: http://%s\n", addr)
	fmt.Println("Press Ctrl+C to stop.")
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
