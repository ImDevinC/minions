// Commitlint configuration
// Enforces conventional commits format for semantic-release compatibility

module.exports = {
  extends: ['@commitlint/config-conventional'],
  rules: {
    // Enforce lowercase type
    'type-case': [2, 'always', 'lower-case'],
    // Disable subject-case rule - allow any casing in subject
    // Many commits use sentence-case which is valid conventional commit style
    'subject-case': [0],
    // Allowed types matching semantic-release config
    'type-enum': [
      2,
      'always',
      [
        'feat',     // New feature -> minor bump
        'fix',      // Bug fix -> patch bump
        'perf',     // Performance improvement -> patch bump
        'refactor', // Code refactoring -> patch bump
        'docs',     // Documentation only
        'style',    // Code style changes (formatting, etc.)
        'test',     // Adding or updating tests
        'chore',    // Maintenance tasks
        'ci',       // CI/CD changes
        'build',    // Build system changes
        'revert',   // Reverting previous commits
      ],
    ],
  },
};
