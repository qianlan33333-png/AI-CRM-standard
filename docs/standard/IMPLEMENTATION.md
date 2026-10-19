# CRM Standard Implementation Plan

Approved parent brief: PRD.md. All work is in this independent repository.

1. [x] Export exact V4 source snapshot; exclude operational data/deploy bindings; record provenance.
2. [x] Build scoped customer context reader/cache and current-fact composition; historical paging and evidence tests.
3. [x] Add product-owned internal operating notes; preserve public response separation; test save/read and product-version behavior.
4. [x] Bind existing generation/review/sidebar to shared context; add scoped/manual and periodic prepared context paths.
5. [x] Product Design: extend existing mounted UI; browser verify save/read, context coverage and empty states.
6. [x] Docker Compose and test2g/standard4g/standard8g profiles; clean bootstrap and local Linux build/migration tests.
7. [x] Create and push private GitHub repository after examining source for runtime/enterprise secrets.
8. [x] Only then stop old target CRM tasks, clear owned old data/config/builds, install verified artifacts and new PostgreSQL volume.
9. [x] Verify domain/TLS/login/empty business counts/no worker/provider activity; record exact artifact and resource receipts.

Current source baseline is in source.json. No existing production or current shared staging host may be used for build or verification.
