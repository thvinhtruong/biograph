# Exam Prep Dashboard

## High-Priority Concepts (by activation weight)

```dataview
TABLE activation_weight AS "Weight", edge_count AS "Connections", sources AS "Sources"
FROM "entities"
WHERE exam_date != null
SORT activation_weight DESC
LIMIT 20
```

## Recently Updated

```dataview
LIST
FROM "entities"
SORT last_updated DESC
LIMIT 10
```

## Weakly Connected (may need review)

```dataview
TABLE activation_weight AS "Weight", edge_count AS "Connections"
FROM "entities"
WHERE activation_weight < 0.3
SORT activation_weight ASC
```
