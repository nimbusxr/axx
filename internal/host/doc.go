// Package host starts a project's services for runs that cannot start them
// themselves: an agent's, in a sandbox that keeps it from Docker.
//
// The person starts `axx host` in the project, on their own machine. It
// listens on a loopback port and announces itself in .axx/host.json (its
// address and a token), which an axx in the sandbox reads through the
// project folder they share. Each `axx run` there asks the host for its
// services: the host cleans up what an earlier run left, starts them as
// axx.yaml says, waits until they are ready and sends their output back;
// the scenarios run in the sandbox; when the run ends, or its connection
// does, the host stops the services and runs their cleanup. Runs take
// their turn: one has the services at a time.
//
// Only a definition the person approved starts: axx.yaml's services, what
// each compose project they run resolves to, and the files their commands
// name ([Define]). Starting the host approves the definition as it is,
// unless it changed since the person last approved one: a run whose
// definition differs waits for their answer, in the host's terminal or in
// the editor that started it, with the difference and what in it reaches
// past a container (privileged containers, mounts of this machine's files,
// the Docker socket, its network). Approvals are kept outside the project,
// where the agent cannot write them.
//
// Nothing a run sends is run as a command: a run names services, and the
// host runs what the approved definition says.
package host
