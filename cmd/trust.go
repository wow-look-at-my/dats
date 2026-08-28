package cmd

// The trust command: manage which .dats files may run their commands on
// which ssh target.

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/dats/internal/sshtrust"
)

var trustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Manage which .dats files may run commands on which ssh host",
	Long: `Manage approvals for the file-level "ssh:" key.

A .dats file naming an ssh target is the one thing in the format that widens
what a run reaches: opening an unfamiliar suite would otherwise dial out
using your keys and agent. An approval is that consent, recorded once.

An approval is keyed on the file's path, not its contents -- a suite under
development changes constantly, and re-approving on every edit would make the
key unusable. So it says "this file may reach this host", never "these
commands may".

A target you type yourself (dats --ssh user@host) needs no approval: typing
it is the approval.`,
	Args: cobra.NoArgs,
}

var trustListCmd = &cobra.Command{
	Use:   "list",
	Short: "List approved file/host pairs",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		store, err := sshtrust.Load()
		if err != nil {
			return err
		}
		entries := store.List()
		if len(entries) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "no approved ssh targets")
			return nil
		}
		for _, entry := range entries {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", entry.File, entry.Target)
		}
		return nil
	},
}

var trustAddCmd = &cobra.Command{
	Use:   "add <file.dats> <[user@]host>",
	Short: "Approve one .dats file to run its commands on one ssh host",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := sshtrust.Load()
		if err != nil {
			return err
		}
		if err := store.Approve(args[0], args[1]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "approved %s on %s\n", args[0], args[1])
		return nil
	},
}

var trustRemoveCmd = &cobra.Command{
	Use:   "remove <file.dats> <[user@]host>",
	Short: "Withdraw an approval",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := sshtrust.Load()
		if err != nil {
			return err
		}
		removed, err := store.Revoke(args[0], args[1])
		if err != nil {
			return err
		}
		if !removed {
			return fmt.Errorf("no approval for %s on %s", args[0], args[1])
		}
		fmt.Fprintf(cmd.OutOrStdout(), "removed %s on %s\n", args[0], args[1])
		return nil
	},
}

func init() {
	trustCmd.AddCommand(trustListCmd, trustAddCmd, trustRemoveCmd)
	rootCmd.AddCommand(trustCmd)
}

// approveSSHTarget is the run's answer to a file that named a target. On a
// terminal it asks once and records the answer; with nobody watching it
// refuses and names the command that grants the approval, rather than
// stopping at a prompt no pipeline can answer.
func approveSSHTarget(datsPath, target string) error {
	store, err := sshtrust.Load()
	if err != nil {
		return err
	}
	approved, err := store.Approved(datsPath, target)
	if err != nil {
		return err
	}
	if approved {
		return nil
	}
	if !stdinIsTTY() {
		return fmt.Errorf("ssh: %s asks to run its commands on %s, which is not approved -- approve it with `dats trust add %s %s`", datsPath, target, datsPath, target)
	}

	fmt.Fprintf(os.Stderr, "\n%s asks to run its commands on %s.\n", datsPath, target)
	fmt.Fprintf(os.Stderr, "That spends your ssh credentials on commands you may not have written.\n")
	fmt.Fprintf(os.Stderr, "Approve this file on this host? [y/N] ")

	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return fmt.Errorf("ssh: reading the approval answer: %w", err)
	}
	if strings.ToLower(strings.TrimSpace(answer)) != "y" {
		return fmt.Errorf("ssh: %s is not approved to run on %s", datsPath, target)
	}
	if err := store.Approve(datsPath, target); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "approved; remove it later with `dats trust remove %s %s`\n\n", datsPath, target)
	return nil
}

// stdinIsTTY reports whether a person is present to answer a prompt.
func stdinIsTTY() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
