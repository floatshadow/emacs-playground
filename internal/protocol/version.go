package protocol

// Version is the protocol version carried as "protocol" in the events hello
// (api.md §4) and the agent hello (api.md §7). Breaking changes to the
// events, RPC or agent types bump it; the REST prefix changes only when the
// gateway REST API breaks.
const Version = 2

// APIPrefix is the gateway REST prefix (api.md §2).
const APIPrefix = "/api/v1"

// AgentVersion is the playground-agent.el version this protocol revision was
// written against. sessiond logs a warning when the agent reports another
// major version.
const AgentVersion = "0.1.0"

// CurriculumFormat is the version of the built curriculum bundle format
// (curriculum.json). The gateway refuses a bundle with a different format.
const CurriculumFormat = 1
