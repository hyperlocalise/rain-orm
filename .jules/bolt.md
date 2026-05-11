## 2025-05-15 - [Bypassing Intermediate Cache Structures for Direct Row Scanning]
**Learning:** The ORM was buffering all database results into an intermediate `cachedSelectRows` structure (which uses JSON-compatible types like `cachedValue`) before scanning them into the final destination. This caused redundant allocations proportional to `Rows * Columns`. By implementing a direct scanning path from `sql.Rows` to the target using reflection, I reduced allocations by ~45% and improved execution time by ~15% for representative workloads.
**Action:** When implementing a query builder or ORM, always provide a direct scan path for performance-sensitive paths that don't require caching or intermediate processing. Reusing `[]any` scan targets across rows further reduces pressure on the GC.

## 2026-03-29 - [Caching Column Metadata for Hot Scan Loops]
**Learning:** Performing `table.ColumnByName` lookups for every column of every row during scanning created a bottleneck that scaled with $O(Rows \times Columns)$. By moving this metadata resolution to a per-query "plan" phase, we reduced scanning overhead significantly. Additionally, adding a fast-path for single-level reflection (`fieldByIndexAlloc`) reduced loop and slicing overhead for the most common model field mappings.
**Action:** Always pre-calculate and cache metadata (like column definitions and field indices) before entering high-iteration loops like database result scanning. Reflection operations should be specialized for simple paths when possible.

## 2026-03-31 - [Pre-calculating Reflection Metadata for Value Assignment]
**Learning:** Repeatedly checking if a field type implements `sql.Scanner` or recursively unwrapping pointers during row scanning creates measurable overhead in the hot loop. By moving these reflection-based decisions into the "plan" phase (once per query), we reduced execution time for bulk scans by ~7.5%.
**Action:** Identify reflection checks that are constant for a given type/field and pre-calculate them outside of high-iteration loops. Use a "plan" structure to carry this metadata into the loop.
