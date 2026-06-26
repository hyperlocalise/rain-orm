## 2025-03-29 - Avoiding redundant object cloning in query helpers
**Learning:** High-frequency query helpers like `First()` often perform minimal modifications (e.g., `LIMIT 1`). Cloning the entire query builder (`clone()`) is expensive because it triggers deep copies of multiple slices (WHERE, ORDER BY, JOINs, etc.).
**Action:** Use temporary in-place mutation with `defer` to restore state for lightweight helpers instead of full object cloning, unless thread-safety is an explicit requirement for that specific call path.
