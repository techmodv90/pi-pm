# Source

This family is derived from [`samber/cc-skills-golang`](https://github.com/samber/cc-skills-golang), licensed MIT.

Conversion for Pi task workers:

- removes Claude Code frontmatter, commands, workflow assets, and router behavior;
- preserves Go-specific guidance, examples, references, and non-Claude assets;
- keeps child skill names prefixed with `golang-` for Pi's flat skill namespace;
- excludes source evaluation fixtures because they are not runtime guidance.
