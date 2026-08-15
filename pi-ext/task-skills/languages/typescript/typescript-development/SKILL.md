---
name: typescript-development
description: TypeScript development with strict typing, advanced type utilities, and modern patterns. Use when working with TypeScript files (.ts, .tsx), building typed JavaScript applications, or implementing type-safe APIs.
---

# TypeScript

## Core Patterns

### Type Definitions

```typescript
// Interfaces for objects
interface User {
  id: string;
  email: string;
  name: string;
  createdAt: Date;
}

// Types for unions and utilities
type Status = 'pending' | 'active' | 'inactive';
type UserWithStatus = User & { status: Status };

// Generic types
type ApiResponse<T> = {
  data: T;
  error?: string;
  status: number;
};
```

### Utility Types

```typescript
// Partial - all properties optional
type UserUpdate = Partial<User>;

// Pick - select properties
type UserPreview = Pick<User, 'id' | 'name'>;

// Omit - exclude properties
type UserWithoutId = Omit<User, 'id'>;

// Record - dictionary type
type UserMap = Record<string, User>;
```

### Async Patterns

```typescript
async function fetchUser(id: string): Promise<User> {
  const response = await fetch(`/api/users/${id}`);
  if (!response.ok) {
    throw new Error(`Failed to fetch user: ${response.status}`);
  }
  return response.json();
}

// Error handling
async function safeOperation<T>(
  operation: () => Promise<T>
): Promise<[T, null] | [null, Error]> {
  try {
    const result = await operation();
    return [result, null];
  } catch (error) {
    return [null, error as Error];
  }
}
```

### Class Patterns

```typescript
class UserService {
  constructor(private readonly db: Database) {}

  async findById(id: string): Promise<User | null> {
    return this.db.users.findUnique({ where: { id } });
  }

  async create(data: UserCreate): Promise<User> {
    return this.db.users.create({ data });
  }
}
```

### Zod Validation

```typescript
import { z } from 'zod';

const UserSchema = z.object({
  email: z.string().email(),
  name: z.string().min(1).max(100),
  password: z.string().min(8),
});

type UserInput = z.infer<typeof UserSchema>;

function validateUser(data: unknown): UserInput {
  return UserSchema.parse(data);
}
```

## Best Practices

1. Enable strict mode in tsconfig.json
2. Avoid `any` - use `unknown` and type guards
3. Use interfaces for object shapes, types for unions
4. Prefer `const` assertions for literal types
5. Use discriminated unions for state
6. Never use class, just functions

## Common Pitfalls

- **Using `any`**: Defeats type safety
- **Not handling null/undefined**: Use strict null checks
- **Type assertions**: Prefer type guards
- **Ignoring errors**: Handle all promise rejections
