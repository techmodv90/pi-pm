# Vertical Slice Groups And Bite-Sized Tasks

**Status**: accepted

Features represent complete vertical slices, while their child Tasks are bite-sized, requirement-bound increments. Each Task supplies focused Child Evidence and passes Code Review against both the Task contract and repository standards; Aggregate Verification remains the final integrated QA gate for the Feature or Epic. This keeps Tasks small enough to implement and review without losing an end-to-end acceptance boundary. We reject both one oversized Task per slice and removing child Code Review in favor of aggregate QA alone.
