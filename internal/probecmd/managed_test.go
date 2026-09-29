package probecmd

import (
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestManagedObservationRechecksWholeBoundaryWhenIdleEngineDisappears(t *testing.T) {
	for _, disappearAt := range []string{"executable", "arguments", "listener"} {
		t.Run(disappearAt, func(t *testing.T) {
			root := processRow{pid: 100, ppid: 1, pgid: 100, started: "router", state: "S"}
			child := processRow{pid: 101, ppid: 100, pgid: 101, started: "engine", state: "S"}
			snapshot := 0
			gone := os.NewSyscallError("process identity", syscall.ESRCH)
			reader := managedInspector{
				rows: func() ([]processRow, error) {
					snapshot++
					if snapshot == 1 {
						return []processRow{root, child}, nil
					}
					return []processRow{root}, nil
				},
				executable: func(pid int) (string, error) {
					if pid == child.pid && disappearAt == "executable" {
						return "", gone
					}
					return map[int]string{100: "/router", 101: "/engine"}[pid], nil
				},
				arguments: func(pid int) (string, error) {
					if pid == child.pid && disappearAt == "arguments" {
						return "", gone
					}
					return map[int]string{100: "/router\x00--config\x00/config", 101: "/engine\x00--model\x00/model"}[pid], nil
				},
				listeners: func([]processRow, int, string) (bool, error) {
					if snapshot == 1 && disappearAt == "listener" {
						return false, gone
					}
					return true, nil
				},
			}
			got, err := reader.observe(100, "/router", []string{"--config", "/config"}, "127.0.0.1:18088", []ManagedCommand{{ID: "writer", Path: "/engine", Arguments: []string{"--model", "/model"}}})
			if err != nil || !got.Listening || len(got.Identities) != 1 || got.Identities[0].ID != "router" {
				t.Fatalf("idle unload did not produce a fresh verified observation: %+v, %v", got, err)
			}
		})
	}
}

func TestManagedObservationRetriesOnlyDisappearanceAndRemainsBounded(t *testing.T) {
	for _, failure := range []error{syscall.ESRCH, syscall.EPERM, errors.New("unrecognized managed child")} {
		t.Run(failure.Error(), func(t *testing.T) {
			reads := 0
			reader := managedInspector{rows: func() ([]processRow, error) {
				reads++
				return nil, failure
			}}
			_, err := reader.observe(100, "/router", nil, "127.0.0.1:18088", nil)
			if !errors.Is(err, failure) {
				t.Fatalf("inspection failure became availability: %v", err)
			}
			want := 1
			if errors.Is(failure, syscall.ESRCH) {
				want = 3
			}
			if reads != want {
				t.Fatalf("read %d snapshots, want %d", reads, want)
			}
		})
	}
}

func TestManagedRecoveryFollowsRecordedGroupsAndNeverAdoptsReusedPIDs(t *testing.T) {
	root := ManagedIdentity{ProcessIdentity: ProcessIdentity{ID: "splash", PID: 100, PGID: 100, Started: "old"}, Path: "/python", Arguments: "/python\x00--port\x0010001"}
	commands := []ManagedCommand{{ID: "splash", Path: "/python", Arguments: []string{"--port", "${PORT}"}}, {ID: "splash/engine", Path: "/splash", Arguments: []string{"serve"}}}
	for _, scenario := range []string{"idle", "live", "orphan", "reused", "unknown", "wrong-argv"} {
		t.Run(scenario, func(t *testing.T) {
			rows := []processRow{}
			paths, args := map[int]string{}, map[int]string{}
			if scenario != "idle" {
				rows = append(rows, processRow{pid: 101, ppid: 100, pgid: 100, started: "child", state: "S"})
				paths[101], args[101] = "/splash", "/splash\x00serve"
			}
			if scenario == "live" || scenario == "reused" {
				start := "old"
				if scenario == "reused" {
					start = "new"
				}
				rows = append(rows, processRow{pid: 100, ppid: 1, pgid: 100, started: start, state: "S"})
				paths[100], args[100] = "/python", root.Arguments
			}
			if scenario == "unknown" {
				paths[101] = "/unrelated"
			}
			if scenario == "wrong-argv" {
				args[101] = "/splash\x00different"
			}
			read := func(values map[int]string) func(int) (string, error) {
				return func(pid int) (string, error) {
					s, ok := values[pid]
					if !ok {
						return "", errors.New("absent")
					}
					return s, nil
				}
			}
			got, err := observeManagedLoads(rows, []ManagedIdentity{root}, commands, read(paths), read(args))
			if scenario == "unknown" || scenario == "wrong-argv" {
				if err == nil {
					t.Fatal("unrelated process became recovery authority")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]int{"idle": 0, "live": 2, "orphan": 1, "reused": 0}[scenario]
			if len(got) != want {
				t.Fatalf("observed %d, want %d", len(got), want)
			}
		})
	}
}

func TestManagedRecoveryRequiresCompleteSelectedCommand(t *testing.T) {
	command := ManagedCommand{ID: "writer", Path: "/engine", Arguments: []string{"--model", "/selected", "--port", "${PORT}"}}
	identity := ManagedIdentity{ProcessIdentity: ProcessIdentity{ID: "writer", PID: 10, PGID: 10, Started: "exact"}, Path: "/engine", Arguments: "/engine\x00--model\x00/selected\x00--port\x0010001"}
	if !ManagedIdentityAllowed(identity, []ManagedCommand{command}) {
		t.Fatal("exact reload refused")
	}
	identity.Arguments = "/engine\x00--model\x00/unselected\x00--port\x0010001"
	if ManagedIdentityAllowed(identity, []ManagedCommand{command}) {
		t.Fatal("path-only authority accepted")
	}
}
