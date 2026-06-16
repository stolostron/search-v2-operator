sessions:
---
- date: "2026-06-15"
  title: "[search-mcp] SAR-09: Plan for creating and using a read-only PostgreSQL user"
  jira: "ACM-35503"
  jira_url: "https://redhat.atlassian.net/browse/ACM-35503"
  pr: "https://github.com/stolostron/search-v2-operator/pull/737"
  plan: "plans/ACM-32474-readonly-postgres-user-plan.md"
  summary: "Implemented search_api_ro and search_mcp_ro PostgreSQL roles provisioned by the operator at database startup, with new create-only Secrets and the NOTIFY trigger moved from search-v2-api into postgresql.sql"
