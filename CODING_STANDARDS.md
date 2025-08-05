### 1. **Coding Standards Drafts**
- Clear, enforceable drafts outlining standards for:
    - File and directory organization (e.g., one file per related set of functions/types; directories by domain, not technology)
    - Naming conventions (lowerCamelCase for variables/methods, UpperCamelCase for types, snake_case for files; consistent acronym casing)
    - Documentation (Go-style doc comments for all exported items, comments that explain *why*, not just *what*)
    - Function structure (short functions, single responsibility, explicit error handling, no panics except in main or justified places)
    - Error handling (explicit, contextual errors, use of sentinel errors)
    - Dependency management (all in go.mod, no unused imports)
    - Testing (all exported methods/functions unit-tested, table-driven tests for branches, location of tests, use of mocks as needed)

---

### 2. **Project and Process Enforcement**
- Mandatory use of specific Go versions (e.g., Go 1.24.4), CI/CD enforcement (no bypassing lint/tests), and use of official Docker images
- Restriction of long-lived branches to `main` and `staging`, with all work occurring on short-lived feature branches. Hotfix and versioning process specified
- All merges require code review by an (often AI-powered) reviewer checking lint/tests/logic/contracts/dependencies
- Commit messages must be clear, reference issues, and use a conventional format
- Dependencies reviewed, pinned, and cleaned up via automation

---

### 3. **Clean Code and Structure**
- Clear separation of concerns: domain, data access, and interfaces are separate; no mixing business logic with transport/infrastructure
- Each file has a single responsibility; no huge files or god functions
- Adherence to idiomatic Go practices and avoidance of magic numbers/values; strong architectural layering (e.g., Clean Architecture)

---

### 4. **Testing, Quality, and CI/CD**
- High coverage requirements (e.g., 85% unit test coverage, 80% mutation coverage for business logic)
- All test code should be structured and clear, avoiding reliance on external systems when not required (use mocks/fakes)
- Integration and contract tests for APIs, table-driven test patterns, use of fixtures and teardown
- Coverage thresholds enforced by CI
- Formatting via `gofmt` and linting (`golangci-lint`, etc.) required before merging

---

### 5. **Error Handling and Security**
- Comprehensive use of idiomatic error wrapping and custom error types
- Standardized error response format (example JSON provided)
- No panics except for fatal cases; explicit error handling everywhere
- No secrets/credentials in code, enforcement of secure configs and environment handling, static security scans in CI

---

### 6. **Documentation and Non-Functional Requirements**
- All exported items thoroughly documented
- Architecture and rationale for each choice explained, with alternatives considered
- Documentation must stay in sync with code and contracts (OpenAPI/Protobuf) and be audited regularly

---


