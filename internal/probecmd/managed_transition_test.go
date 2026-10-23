package probecmd

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestManagedObservationWaitsForOnlyExactOwnedExec(t *testing.T) {
	command := ManagedCommand{ID: "engine", Path: "/engine", Arguments: []string{"--port", "${PORT}"},
		LauncherPath: "/owned/temper", LauncherArguments: []string{"internal-managed-exec", "/owned/launch.json", "${PORT}"}}
	for _, scenario := range []string{"launcher", "crossed-exec", "wrong-path", "wrong-record", "wrong-port", "wrong-destination", "stuck"} {
		t.Run(scenario, func(t *testing.T) {
			reads := 0
			reader := managedInspector{
				rows: func() ([]processRow, error) {
					reads++
					return []processRow{{pid: 100, ppid: 1, pgid: 100, started: "router", state: "S"}, {pid: 101, ppid: 100, pgid: 101, started: "engine", state: "S"}}, nil
				},
				executable: func(pid int) (string, error) {
					if pid == 100 {
						return "/router", nil
					}
					if scenario == "wrong-path" {
						return "/unrelated/temper", nil
					}
					if reads > 1 && scenario != "stuck" {
						return "/engine", nil
					}
					return command.LauncherPath, nil
				},
				arguments: func(pid int) (string, error) {
					if pid == 100 {
						return "/router", nil
					}
					if scenario == "crossed-exec" || (reads > 1 && scenario != "stuck") {
						return "/engine\x00--port\x0012433", nil
					}
					args := "/owned/temper\x00internal-managed-exec\x00/owned/launch.json\x0012433"
					switch scenario {
					case "wrong-record":
						args = strings.Replace(args, "/owned/launch.json", "/other/launch.json", 1)
					case "wrong-port":
						args = strings.Replace(args, "12433", "80", 1)
					case "wrong-destination":
						args = "/engine\x00--different\x0012433"
					}
					return args, nil
				},
				listeners: func([]processRow, int, string) (bool, error) { return true, nil },
			}
			started := time.Now()
			got, err := reader.observe(100, "/router", nil, "127.0.0.1:18090", []ManagedCommand{command})
			switch scenario {
			case "launcher", "crossed-exec":
				if err != nil || reads != 2 || len(got.Identities) != 2 || got.Identities[1].Path != "/engine" {
					t.Fatal(got, reads, err)
				}
			case "stuck":
				if !errors.Is(err, errManagedExec) || len(got.Identities) != 0 || time.Since(started) > 3*time.Second {
					t.Fatal(got, err)
				}
			default:
				if err == nil || errors.Is(err, errManagedExec) || reads != 1 {
					t.Fatal("unknown command was retried/adopted", reads, err)
				}
			}
		})
	}
}

func TestRecordedEngineDuringExecNeverGrantsLauncherSignalAuthority(t *testing.T) {
	command := ManagedCommand{ID: "engine", Path: "/engine", Arguments: []string{"--port", "${PORT}"},
		LauncherPath: "/owned/temper", LauncherArguments: []string{"internal-managed-exec", "/owned/launch.json", "${PORT}"}}
	root := ManagedIdentity{ProcessIdentity: ProcessIdentity{ID: "engine", PID: 100, PGID: 100, Started: "same"}, Path: "/engine", Arguments: "/engine\x00--port\x0012433"}
	row := processRow{pid: 100, pgid: 100, started: "same", state: "S"}
	args := "/owned/temper\x00internal-managed-exec\x00/owned/launch.json\x0012433"
	got, err := observeManagedLoads([]processRow{row}, []ManagedIdentity{root}, []ManagedCommand{command}, func(int) (string, error) { return command.LauncherPath, nil }, func(int) (string, error) { return args, nil })
	if !errors.Is(err, errManagedExec) || len(got) != 0 {
		t.Fatal(got, err)
	}
	root.Path, root.Arguments = command.LauncherPath, args
	if ManagedIdentityAllowed(root, []ManagedCommand{command}) {
		t.Fatal("launcher became persisted signaling authority")
	}
}

func TestManagedObservationNeverAdoptsASecondRouter(t *testing.T) {
	reader := managedInspector{
		rows: func() ([]processRow, error) {
			return []processRow{{pid: 100, ppid: 1, pgid: 100, state: "S"}, {pid: 101, ppid: 100, pgid: 101, state: "S"}}, nil
		},
		executable: func(int) (string, error) { return "/router", nil },
		arguments:  func(int) (string, error) { return "/router", nil },
	}
	_, err := reader.observe(100, "/router", nil, "127.0.0.1:18090", []ManagedCommand{{ID: "router", Path: "/router"}})
	if err == nil || !strings.Contains(err.Error(), "unrecognized managed child") {
		t.Fatal("child inherited router authority", err)
	}
}
