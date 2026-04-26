# Exam Prep Dashboard

## Active Exam Radar

Concepts tied to exams within the next 30 days.

```dataview
TABLE course AS "Course", exam_date AS "Exam Date", category AS "Type"
FROM "entities"
WHERE exam_date != null AND exam_date <= (date(today) + dur(30 days)) AND exam_date >= date(today)
SORT exam_date ASC
```

## Core Knowledge Hubs

Highest-connectivity nodes — attract the most energy during spreading activation.

```dataview
TABLE category AS "Type", course AS "Course", length(file.inlinks) AS "Inbound Edges"
FROM "entities"
WHERE length(file.inlinks) > 0
SORT length(file.inlinks) DESC
LIMIT 10
```

## Orphaned Nodes

Entities with no connections — candidates for manual linking or review.

```dataview
TABLE category AS "Type", course AS "Course"
FROM "entities"
WHERE length(file.inlinks) = 0 AND length(file.outlinks) = 0
SORT last_updated DESC
```
