sessions:
---
- date: "2026-06-15"
  title: "[search-mcp] SAR-09: Plan for creating and using a read-only PostgreSQL user"
  jira: "ACM-35503"
  jira_url: "https://redhat.atlassian.net/browse/ACM-35503"
  pr: ~
  plan: "plans/ACM-32474-readonly-postgres-user-plan.md"
  summary: "Plan search-v2-operator changes to provision dedicated read-only PostgreSQL roles for both search-v2-api and search-mcp-server, eliminating their use of the shared read-write searchuser credential"
