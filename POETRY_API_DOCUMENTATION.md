# Poetry Backend API Documentation

Complete API reference for the Poetry App Phase 1 features: User Profile Setup and Poem CRUD operations.

---

## Table of Contents

1. [Profile Setup APIs](#profile-setup-apis)
   - [POST /users/setup](#post-userssetup) - Complete profile after first login
   - [GET /users/username/check](#get-usersusernamecheck) - Check username availability
   - [POST /users/username](#post-usersusername) - Set username permanently
2. [Poem APIs](#poem-apis)
   - [POST /poems](#post-poems) - Create a new poem
   - [GET /poems/me](#get-poemsme) - Get my poems (includes drafts)
   - [GET /poems/user/:userId](#get-poemsuseruserid) - Get user's public poems
   - [GET /poems/:id](#get-poemsid) - Get a single poem
   - [PATCH /poems/:id](#patch-poemsid) - Update a poem
   - [DELETE /poems/:id](#delete-poemsid) - Delete a poem

---

## Authentication

Most endpoints require Firebase Authentication. Include the ID token in the Authorization header:

```
Authorization: Bearer <firebase_id_token>
```

Some endpoints marked as "Optional Auth" work both with and without authentication:
- Without auth: Only public poems are returned
- With auth: Can see own private poems when viewing your own profile

---

## Profile Setup APIs

### POST /api/v1/users/setup

**Auth Required:** Yes

Complete profile setup after first login. This marks `isProfileSetup: true` and allows the user to start creating poems.

**Request Body:**

```json
{
  "displayName": "Asif Writes",
  "bio": "Poet | Dreamer | Storyteller",
  "externalLink": "https://instagram.com/asif_writes",
  "photoURL": "https://cloudinary.com/.../profile.jpg",
  "coverImageURL": "https://cloudinary.com/.../cover.jpg"
}
```

**Field Details:**

| Field | Type | Required | Validation |
|-------|------|----------|------------|
| `displayName` | string | Yes | 1-50 characters |
| `bio` | string | No | Max 200 characters |
| `externalLink` | string | No | Must start with http:// or https:// |
| `photoURL` | string | No | Valid URL |
| `coverImageURL` | string | No | Valid URL |

**Response (200 OK):**

```json
{
  "status": "success",
  "message": "Profile setup complete",
  "data": {
    "id": "65f123abc...",
    "firebaseUid": "...",
    "email": "asif@example.com",
    "displayName": "Asif Writes",
    "photoURL": "https://cloudinary.com/.../profile.jpg",
    "bio": "Poet | Dreamer | Storyteller",
    "username": "",
    "externalLink": "https://instagram.com/asif_writes",
    "coverImageURL": "https://cloudinary.com/.../cover.jpg",
    "isProfileSetup": true,
    "isEditor": false,
    "postsCount": 0,
    "createdAt": "2024-01-15T10:30:00Z",
    "updatedAt": "2024-01-15T10:45:00Z"
  }
}
```

**Error Responses:**

- `400 Bad Request` - Validation error (e.g., bio too long, invalid external link)
- `401 Unauthorized` - Missing or invalid token

---

### GET /api/v1/users/username/check

**Auth Required:** No

Check if a username is available. Call this on every keystroke (debounce on frontend) to give real-time feedback.

**Query Parameters:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `username` | string | Yes | Username to check (3-30 chars, lowercase, alphanumeric + underscore) |

**Request:**

```
GET /api/v1/users/username/check?username=asif_writes
```

**Response (200 OK):**

```json
{
  "status": "success",
  "message": "Username check result",
  "data": {
    "username": "asif_writes",
    "available": true,
    "reason": ""
  }
}
```

**Unavailable Username Response:**

```json
{
  "status": "success",
  "message": "Username check result",
  "data": {
    "username": "admin",
    "available": false,
    "reason": "reserved"
  }
}
```

**Reason Values:**

| Reason | Meaning |
|--------|---------|
| `""` (empty) | Available |
| `"taken"` | Already claimed by another user |
| `"reserved"` | Reserved system name (e.g., "admin", "support") |
| `"invalid_format"` | Doesn't match pattern `^[a-z0-9_]{3,30}$` |

**Reserved Usernames:**
`admin`, `support`, `editor`, `chatbee`, `poetry`, `official`, `moderator`, `help`, `me`, `settings`, `explore`, `feed`, `search`, `notifications`, `profile`

---

### POST /api/v1/users/username

**Auth Required:** Yes

Permanently set the username. **Can only be done once** — username cannot be changed after setting.

**Request Body:**

```json
{
  "username": "asif_writes"
}
```

**Validation Rules:**
- 3-30 characters
- Lowercase letters, numbers, and underscores only
- Must not be reserved
- Must be available (not taken)

**Response (200 OK):**

```json
{
  "status": "success",
  "message": "Username set successfully",
  "data": {
    "id": "65f123abc...",
    "username": "asif_writes",
    "isProfileSetup": true,
    ...
  }
}
```

**Error Responses:**

- `400 Bad Request` - Invalid format, reserved, or already taken
- `400 Bad Request` - Username already set (can only set once)
- `401 Unauthorized` - Missing or invalid token

---

## Poem APIs

### POST /api/v1/poems

**Auth Required:** Yes

Create a new poem. Can be published immediately (`visibility: "public"`) or saved as draft (`visibility: "private"`).

**Request Body:**

```json
{
  "title": "Midnight Thoughts",
  "contentJson": "{\"ops\":[{\"insert\":\"The moon whispers...\"}]}",
  "plainText": "The moon whispers secrets to the night...",
  "hashtags": ["poetry", "night", "moon", "thoughts"],
  "mood": "nostalgia",
  "isOriginal": true,
  "visibility": "public",
  "audioUrl": "https://cloudinary.com/.../audio.mp3",
  "audioDuration": 45,
  "coverColor": "#1a1a2e"
}
```

**Field Details:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `title` | string | No | Defaults to "Untitled Poem". Max 200 chars |
| `contentJson` | string | **Yes** | Quill Delta JSON format |
| `plainText` | string | No | Stripped plain text for search indexing |
| `hashtags` | array | No | Max 10 tags. Stored lowercase, without # |
| `mood` | string | No | Must be one of the valid moods (see below) |
| `isOriginal` | boolean | No | Copyright checkbox - user's original work |
| `visibility` | string | No | `"public"` or `"private"`. Defaults to `"public"` |
| `audioUrl` | string | No | Cloudinary URL for audio recording |
| `audioDuration` | integer | No | Duration in seconds (0 if no audio) |
| `coverColor` | string | No | Hex color code from editor |

**Valid Moods:**
`love`, `grief`, `nature`, `nostalgia`, `hope`, `dark`, `spiritual`, `humour`, `life`, `longing`

**Response (201 Created):**

```json
{
  "status": "success",
  "message": "Poem created",
  "data": {
    "id": "65f456def...",
    "author": {
      "id": "65f123abc...",
      "displayName": "Asif Writes",
      "username": "asif_writes",
      "photoURL": "https://cloudinary.com/.../profile.jpg",
      "isEditor": false
    },
    "title": "Midnight Thoughts",
    "contentJson": "{\"ops\":[{\"insert\":\"The moon whispers...\"}]}",
    "plainText": "The moon whispers secrets to the night...",
    "hashtags": ["poetry", "night", "moon", "thoughts"],
    "mood": "nostalgia",
    "isOriginal": true,
    "visibility": "public",
    "audioUrl": "https://cloudinary.com/.../audio.mp3",
    "audioDuration": 45,
    "coverColor": "#1a1a2e",
    "likesCount": 0,
    "commentsCount": 0,
    "repostsCount": 0,
    "createdAt": "2024-01-15T11:00:00Z",
    "updatedAt": "2024-01-15T11:00:00Z"
  }
}
```

**Error Responses:**

- `400 Bad Request` - Missing content, invalid mood, title too long
- `401 Unauthorized` - Missing or invalid token

---

### GET /api/v1/poems/me

**Auth Required:** Yes

Get all poems by the authenticated user. Includes both public and private (draft) poems.

**Query Parameters:**

| Param | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `limit` | integer | No | 20 | Number of poems to fetch (max 50) |
| `before` | string | No | - | Cursor for pagination (ObjectID of last poem) |

**Request:**

```
GET /api/v1/poems/me?limit=20
GET /api/v1/poems/me?limit=20&before=65f456def...
```

**Response (200 OK):**

```json
{
  "status": "success",
  "message": "My poems retrieved",
  "data": {
    "poems": [
      {
        "id": "65f456def...",
        "author": { ... },
        "title": "Midnight Thoughts",
        "contentJson": "...",
        "plainText": "...",
        "hashtags": ["poetry", "night"],
        "mood": "nostalgia",
        "isOriginal": true,
        "visibility": "public",
        "audioUrl": "...",
        "audioDuration": 45,
        "coverColor": "#1a1a2e",
        "likesCount": 12,
        "commentsCount": 3,
        "repostsCount": 1,
        "createdAt": "2024-01-15T11:00:00Z",
        "updatedAt": "2024-01-15T11:00:00Z"
      },
      {
        "id": "65f789ghi...",
        "title": "Draft Poem",
        "visibility": "private",
        ...
      }
    ],
    "hasMore": true
  }
}
```

**Pagination Notes:**
- Returns poems sorted by newest first (by `_id` descending)
- Pass the last poem's `id` as `before` to fetch the next page
- `hasMore: true` indicates there are more poems to fetch

---

### GET /api/v1/poems/user/:userId

**Auth Required:** Optional

Get public poems by a specific user. If authenticated and viewing your own profile, includes private poems too.

**Path Parameters:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `userId` | string | Yes | User's MongoDB ObjectID |

**Query Parameters:**

| Param | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `limit` | integer | No | 20 | Number of poems to fetch (max 50) |
| `before` | string | No | - | Cursor for pagination |

**Request:**

```
GET /api/v1/poems/user/65f123abc...?limit=20
```

**Response:** Same format as `GET /poems/me`

**Behavior:**
- Without auth: Returns only `visibility: "public"` poems
- With auth (viewing own profile): Returns all poems (public + private)
- With auth (viewing other user): Returns only public poems

---

### GET /api/v1/poems/:id

**Auth Required:** Optional

Get a single poem by ID.

**Path Parameters:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Poem's MongoDB ObjectID |

**Request:**

```
GET /api/v1/poems/65f456def...
```

**Response (200 OK):**

```json
{
  "status": "success",
  "message": "Poem retrieved",
  "data": {
    "id": "65f456def...",
    "author": {
      "id": "65f123abc...",
      "displayName": "Asif Writes",
      "username": "asif_writes",
      "photoURL": "...",
      "isEditor": false
    },
    "title": "Midnight Thoughts",
    "contentJson": "...",
    ...
  }
}
```

**Error Responses:**

- `400 Bad Request` - Invalid poem ID or poem not found
- `400 Bad Request` - Poem is private and user is not the author

---

### PATCH /api/v1/poems/:id

**Auth Required:** Yes

Update an existing poem. Only the author can update. All fields are optional — only provided fields are updated.

**Path Parameters:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Poem's MongoDB ObjectID |

**Request Body:**

```json
{
  "title": "Midnight Thoughts (Revised)",
  "contentJson": "{\"ops\":[{\"insert\":\"The moon still whispers...\"}]}",
  "plainText": "The moon still whispers secrets...",
  "hashtags": ["poetry", "night", "revised"],
  "mood": "nostalgia",
  "isOriginal": true,
  "visibility": "public",
  "audioUrl": "https://cloudinary.com/.../new_audio.mp3",
  "audioDuration": 50,
  "coverColor": "#16213e"
}
```

**Notes:**
- Send only the fields you want to update
- Unchanged fields can be omitted
- Hashtag counts are automatically updated (old hashtags decremented, new ones incremented)

**Response (200 OK):**

```json
{
  "status": "success",
  "message": "Poem updated",
  "data": {
    "id": "65f456def...",
    "title": "Midnight Thoughts (Revised)",
    "updatedAt": "2024-01-15T12:00:00Z",
    ...
  }
}
```

**Error Responses:**

- `400 Bad Request` - Invalid poem ID, poem not found, or invalid data
- `400 Bad Request` - User is not the author (unauthorized)
- `401 Unauthorized` - Missing or invalid token

---

### DELETE /api/v1/poems/:id

**Auth Required:** Yes

Soft delete a poem. Only the author can delete.

**Path Parameters:**

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Poem's MongoDB ObjectID |

**Request:**

```
DELETE /api/v1/poems/65f456def...
```

**Response (200 OK):**

```json
{
  "status": "success",
  "message": "Poem deleted",
  "data": null
}
```

**Notes:**
- Poem is soft deleted (marked `isDeleted: true`)
- Associated hashtag counts are decremented
- User's `postsCount` is decremented

**Error Responses:**

- `400 Bad Request` - Invalid poem ID, poem not found
- `400 Bad Request` - User is not the author (unauthorized)
- `401 Unauthorized` - Missing or invalid token

---

## Frontend Integration Guide

### 1. Profile Setup Flow

```javascript
// After first login, check if profile is complete
const checkProfile = async () => {
  const me = await fetch('/api/v1/users/me', { headers: { Authorization: `Bearer ${token}` } });
  if (!me.data.isProfileSetup) {
    // Redirect to profile setup screen
    navigate('/setup-profile');
  }
};

// Complete profile setup
const setupProfile = async (profileData) => {
  const response = await fetch('/api/v1/users/setup', {
    method: 'POST',
    headers: { 
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`
    },
    body: JSON.stringify({
      displayName: profileData.displayName,
      bio: profileData.bio,
      externalLink: profileData.externalLink,
      photoURL: profileData.photoURL,
      coverImageURL: profileData.coverImageURL
    })
  });
  return response.data;
};
```

### 2. Username Selection Flow

```javascript
// Debounced username check (call on keystroke with 300ms debounce)
const checkUsername = debounce(async (username) => {
  const response = await fetch(`/api/v1/users/username/check?username=${username}`);
  return response.data; // { available: true/false, reason: "..." }
}, 300);

// Set username permanently
const setUsername = async (username) => {
  const response = await fetch('/api/v1/users/username', {
    method: 'POST',
    headers: { 
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`
    },
    body: JSON.stringify({ username })
  });
  return response.data;
};
```

### 3. Create Poem Flow

```javascript
// Create new poem
const createPoem = async (poemData) => {
  const response = await fetch('/api/v1/poems', {
    method: 'POST',
    headers: { 
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`
    },
    body: JSON.stringify({
      title: poemData.title,
      contentJson: JSON.stringify(poemData.quillDelta),
      plainText: poemData.plainText,
      hashtags: poemData.hashtags, // ['poetry', 'love', 'night']
      mood: poemData.mood, // 'nostalgia'
      isOriginal: poemData.isOriginal,
      visibility: poemData.visibility, // 'public' or 'private'
      audioUrl: poemData.audioUrl,
      audioDuration: poemData.audioDuration,
      coverColor: poemData.coverColor
    })
  });
  return response.data;
};
```

### 4. Paginated List with Infinite Scroll

```javascript
// Fetch my poems with pagination
const fetchMyPoems = async (before = null, limit = 20) => {
  const url = before 
    ? `/api/v1/poems/me?limit=${limit}&before=${before}`
    : `/api/v1/poems/me?limit=${limit}`;
    
  const response = await fetch(url, {
    headers: { Authorization: `Bearer ${token}` }
  });
  
  return {
    poems: response.data.poems,
    hasMore: response.data.hasMore,
    nextCursor: response.data.poems[response.data.poems.length - 1]?.id
  };
};

// Infinite scroll implementation
const [poems, setPoems] = useState([]);
const [hasMore, setHasMore] = useState(true);
const [cursor, setCursor] = useState(null);

const loadMore = async () => {
  if (!hasMore) return;
  const result = await fetchMyPoems(cursor);
  setPoems(prev => [...prev, ...result.poems]);
  setHasMore(result.hasMore);
  setCursor(result.nextCursor);
};
```

### 5. View User Profile (Public)

```javascript
// Fetch public poems for any user
const fetchUserPoems = async (userId, before = null) => {
  const url = before
    ? `/api/v1/poems/user/${userId}?limit=20&before=${before}`
    : `/api/v1/poems/user/${userId}?limit=20`;
    
  // Optional auth - works without token for public poems
  const headers = token ? { Authorization: `Bearer ${token}` } : {};
  
  const response = await fetch(url, { headers });
  return response.data;
};
```

---

## Error Response Format

All errors follow this standard format:

```json
{
  "status": "error",
  "message": "Descriptive error message",
  "error": {
    "code": "VALIDATION_ERROR",
    "details": "Additional details if available"
  }
}
```

Common HTTP Status Codes:

| Code | Meaning |
|------|---------|
| `200` | Success (OK) |
| `201` | Created successfully |
| `400` | Bad Request - validation error |
| `401` | Unauthorized - missing/invalid token |
| `404` | Not Found |
| `500` | Internal Server Error |

---

## Data Models Reference

### User (Extended)

```json
{
  "id": "65f123abc...",
  "firebaseUid": "...",
  "email": "user@example.com",
  "displayName": "Asif Writes",
  "photoURL": "https://...",
  "bio": "Poet | Dreamer",
  "username": "asif_writes",
  "externalLink": "https://instagram.com/...",
  "coverImageURL": "https://...",
  "isProfileSetup": true,
  "isEditor": false,
  "postsCount": 5,
  "createdAt": "...",
  "updatedAt": "...",
  "lastLoginAt": "...",
  "isActive": true,
  "isBanned": false
}
```

### Poem

```json
{
  "id": "65f456def...",
  "author": {
    "id": "65f123abc...",
    "displayName": "Asif Writes",
    "username": "asif_writes",
    "photoURL": "https://...",
    "isEditor": false
  },
  "title": "Midnight Thoughts",
  "contentJson": "{\"ops\":[...]}",
  "plainText": "The moon whispers...",
  "hashtags": ["poetry", "night"],
  "mood": "nostalgia",
  "isOriginal": true,
  "visibility": "public",
  "audioUrl": "https://...",
  "audioDuration": 45,
  "coverColor": "#1a1a2e",
  "likesCount": 12,
  "commentsCount": 3,
  "repostsCount": 1,
  "createdAt": "...",
  "updatedAt": "..."
}
```

---

**Document Version:** 1.0  
**Last Updated:** 2024-01-15  
**Backend Version:** Poetry Phase 1
