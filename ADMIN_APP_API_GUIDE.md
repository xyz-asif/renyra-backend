# Admin App — Complete API Guide

> Reference for building the **Renyra Admin Flutter app**. Lists every backend
> endpoint, how to authenticate, request/response shapes, and ready-to-paste
> Flutter (Dio) snippets. Written so an AI agent (or a human) can build the app
> end-to-end without reading the Go source.

---

## 1. Basics

| Thing | Value |
|-------|-------|
| Backend stack | Go + Fiber + MongoDB |
| API version prefix | `/api/v1` |
| Local base URL | `http://localhost:8080/api/v1` |
| Production base URL | `https://renyra-backend.onrender.com/api/v1` *(Render service `renyra-backend` — confirm the exact host in your Render dashboard)* |
| Content type | `application/json` |
| Auth scheme | `Authorization: Bearer <accessToken>` (JWT) |

### Rate limits
- **Global:** 100 requests / minute / IP.
- **Write endpoints** (send message, create poem, like, comment, repost, create report): 10 requests / minute / IP.
- Exceeding either returns `429 Too Many Requests` with `{ "error": "..." }`.

### Standard response envelope
Every endpoint (except the health check and a couple of raw ones) returns this shape:

```json
{
  "success": true,
  "statusCode": 200,
  "message": "Human readable message",
  "data": { }
}
```

On error:

```json
{
  "success": false,
  "statusCode": 400,
  "message": "Error description"
}
```

**Flutter rule of thumb:** check `success`; the payload you care about is always under `data`.

---

## 2. Authentication flow

The app uses **Firebase Authentication** as the identity provider, then exchanges
the Firebase ID token for the backend's own **JWT access + refresh token pair**.

```
┌────────────┐   1. Firebase sign-in    ┌────────────────┐
│  Flutter   │ ───────────────────────► │ Firebase Auth  │
│  app       │ ◄─────────────────────── │ (Google/email) │
└────────────┘   Firebase ID token      └────────────────┘
       │
       │  2. POST /auth/exchange { firebaseToken }
       ▼
┌────────────────────────────────────────────┐
│ Backend → { accessToken, refreshToken }     │
└────────────────────────────────────────────┘
       │
       │  3. Use Authorization: Bearer <accessToken> on every protected call
       │  4. On 401 → POST /auth/refresh { refreshToken } → new pair
       ▼
```

### Auth endpoints

| Method | Path | Auth | Body | Returns |
|--------|------|------|------|---------|
| POST | `/auth/exchange` | none | `{ "firebaseToken": "..." }` | `{ accessToken, refreshToken }` |
| POST | `/auth/refresh` | none | `{ "refreshToken": "..." }` | `{ accessToken, refreshToken }` (rotated) |
| POST | `/auth/logout`  | none | `{ "refreshToken": "..." }` | `null` (revokes the refresh token) |

> **Note for an admin-only app:** if you only need the public read endpoints
> (e.g. **List all users**, below), you can skip the whole Firebase flow — those
> endpoints require no token. Login is only needed to call protected endpoints.

---

## 3. ⭐ Admin: List all users (NO AUTH)

This endpoint was added specifically for the admin app. It returns **every user**
in the system (including inactive/banned ones) with their public-safe profile
info — email, photo, join date, counts, etc. **No authentication required.**

```
GET /api/v1/admin/users
```

### Query parameters
| Param | Type | Default | Notes |
|-------|------|---------|-------|
| `q` | string | `""` | Case-insensitive search across `displayName`, `username`, `email`. Empty = all users. |
| `limit` | int | `50` | Max page size. Capped at **200**. |
| `offset` | int | `0` | Number of records to skip (pagination). |
| `sortBy` | string | `createdAt` | One of: `createdAt`, `lastLoginAt`, `displayName`, `followersCount`, `postsCount`. Anything else falls back to `createdAt`. |
| `sortDir` | string | `desc` | `asc` or `desc`. |

### Example request
```
GET /api/v1/admin/users?q=&limit=50&offset=0&sortBy=createdAt&sortDir=desc
```

### Example response
```json
{
  "success": true,
  "statusCode": 200,
  "message": "Users retrieved",
  "data": {
    "users": [
      {
        "id": "665f1a2b3c4d5e6f7a8b9c0d",
        "email": "jane@gmail.com",
        "displayName": "Jane Doe",
        "username": "jane",
        "photoURL": "https://storage.googleapis.com/.../avatar.jpg",
        "coverImageURL": "https://.../cover.jpg",
        "bio": "Poet & dreamer",
        "externalLink": "https://janedoe.com",
        "isProfileSetup": true,
        "isEditor": false,
        "isActive": true,
        "isBanned": false,
        "bannedReason": null,
        "postsCount": 12,
        "followersCount": 340,
        "followingCount": 87,
        "stats": {
          "followersCount": 340,
          "followingCount": 87,
          "totalAnchors": 12,
          "activeAnchors": 10,
          "publicAnchorsCount": 9,
          "totalLikesReceived": 1200,
          "totalClonesReceived": 30,
          "totalCommentsReceived": 210,
          "totalProfileViews": 5400
        },
        "joinedAt": "2024-06-04T10:15:00Z",
        "lastLoginAt": "2026-06-20T08:00:00Z",
        "updatedAt": "2026-06-19T12:00:00Z"
      }
    ],
    "totalCount": 1532,
    "limit": 50,
    "offset": 0,
    "hasMore": true
  }
}
```

### Field reference (per user)
| Field | Meaning |
|-------|---------|
| `id` | Mongo ObjectID (hex string). Use this as the user identifier in all other endpoints. |
| `email` | The user's email (Gmail etc.). |
| `displayName` | Display name. |
| `username` | Handle (may be empty if not set). |
| `photoURL` | Avatar image URL. |
| `coverImageURL` | Profile cover image (optional). |
| `bio` | Profile bio (optional). |
| `externalLink` | User's external link (optional). |
| `isProfileSetup` | Whether onboarding/profile setup is complete. |
| `isEditor` | Whether the user has editor privileges. |
| `isActive` | Whether the account is active. |
| `isBanned` | Whether the account is banned. |
| `bannedReason` | Reason string when banned, else `null`. |
| `postsCount` / `followersCount` / `followingCount` | Top-level counters. |
| `stats` | Denormalized engagement stats object (see example). |
| `joinedAt` | **Date of joining** (account `createdAt`). |
| `lastLoginAt` | Last login timestamp. |
| `updatedAt` | Last profile update timestamp. |

> **Privacy note:** sensitive fields (FCM device tokens, Firebase UID) are
> intentionally **excluded** from this response. Since it has no auth, treat the
> URL as semi-public — restrict access at the network/proxy layer if you need to
> truly lock it down.

### Pagination pattern
```
page 1 → offset=0,   limit=50
page 2 → offset=50,  limit=50
page 3 → offset=100, limit=50
...stop when hasMore == false
```

---

## 4. Full endpoint catalogue

Legend for **Auth** column:
- **none** — public, no token.
- **required** — must send `Authorization: Bearer <accessToken>`.
- **optional** — works without a token, but returns extra personalized fields (e.g. `isLikedByMe`) when a token is present.

### Health
| Method | Path | Auth | Notes |
|--------|------|------|-------|
| GET | `/health` | none | Returns `{ "status": "ok", "mongodb": "connected" }`. Raw JSON (not the standard envelope). |

### Auth
| Method | Path | Auth | Body |
|--------|------|------|------|
| POST | `/auth/exchange` | none | `{ firebaseToken }` |
| POST | `/auth/refresh` | none | `{ refreshToken }` |
| POST | `/auth/logout` | none | `{ refreshToken }` |

### Admin
| Method | Path | Auth | Notes |
|--------|------|------|-------|
| GET | `/admin/users` | **none** | List all users. See §3. |

### Users / profile
| Method | Path | Auth | Notes |
|--------|------|------|-------|
| GET | `/users/me` | required | Current user's full profile. |
| PATCH | `/users/me` | required | Update profile. Body: any of `displayName, photoURL, bio, coverImageURL, externalLink, preferences`. |
| DELETE | `/users/me` | required | Delete account. Body: `{ reason }` (required, ≤1000 chars). |
| POST | `/users/me/fcm-token` | required | Register device push token. Body: `{ token }`. |
| GET | `/users/search?q=&limit=&offset=` | required | Search users (active only). |
| GET | `/users/search-with-status?q=&limit=&offset=` | required | Search + connection status relative to caller. |
| GET | `/users/:id/profile` | optional | Public profile of a user. |
| GET | `/users/:id/followers` | optional | Followers list. |
| GET | `/users/:id/following` | optional | Following list. |
| POST | `/users/:id/follow` | required | Toggle follow/unfollow. |
| POST | `/users/setup` | required | Complete profile setup. |
| GET | `/users/username/check?username=` | none | Check username availability. |
| POST | `/users/username` | required | Set username. Body: `{ username }`. |

### Connections (friend requests)
| Method | Path | Auth | Notes |
|--------|------|------|-------|
| POST | `/connections/request` | required | Send request. Body: `{ receiverId }`. |
| POST | `/connections/:id/accept` | required | Accept a request. |
| POST | `/connections/:id/reject` | required | Reject a request. |
| POST | `/connections/:id/cancel` | required | Cancel a sent request. |
| DELETE | `/connections/:id` | required | Remove an existing connection. |
| GET | `/connections/pending` | required | Pending incoming requests. |
| GET | `/connections/friends` | required | Accepted friends list. |

### Chat & messaging
| Method | Path | Auth | Notes |
|--------|------|------|-------|
| GET | `/chat/rooms` | required | User's chat rooms. |
| POST | `/chat/rooms/direct/:id` | required | Get or create a direct room with user `:id`. |
| GET | `/chat/rooms/:roomId/messages` | required | Messages in a room (paginated via query). |
| POST | `/chat/rooms/:roomId/messages` | required (write-limited) | Send a message. |
| POST | `/chat/rooms/:roomId/read` | required | Mark room as read. |
| DELETE | `/chat/rooms/:roomId` | required | Delete chat. |
| PATCH | `/chat/messages/:messageId/status` | required | Update message status. |
| PUT | `/chat/messages/:messageId/reactions` | required | Add/update reaction. |
| PATCH | `/chat/messages/:messageId` | required | Edit message. |
| DELETE | `/chat/messages/:messageId` | required | Delete message. |
| GET | `/chat/users/:id/presence` | required | Online presence of a user. |
| POST | `/chat/disconnect` | required | Mark self offline. |
| GET (WS) | `/chat/ws` | required | WebSocket upgrade for realtime chat. |

> **WebSocket:** connect to `wss://<host>/api/v1/chat/ws` and pass the access
> token (the upgrade goes through the same `VerifyToken` middleware). Messages
> are JSON `{ type, payload }`. Server pushes types like `profile_updated`,
> new messages, presence, etc.

### Notifications
| Method | Path | Auth | Notes |
|--------|------|------|-------|
| GET | `/notifications/` | required | List notifications. |
| GET | `/notifications/unread-count` | required | Unread badge count. |
| POST | `/notifications/:id/read` | required | Mark one read. |
| POST | `/notifications/read-all` | required | Mark all read. |

### Poems (posts)
| Method | Path | Auth | Notes |
|--------|------|------|-------|
| POST | `/poems/` | required (write-limited) | Create a poem. |
| GET | `/poems/me` | required | Current user's poems. |
| GET | `/poems/user/:userId` | optional | A user's poems. |
| GET | `/poems/:id` | optional | Single poem. |
| PATCH | `/poems/:id` | required | Update own poem. |
| DELETE | `/poems/:id` | required | Delete own poem. |

### Social — likes, comments, reposts
| Method | Path | Auth | Notes |
|--------|------|------|-------|
| POST | `/poems/:id/like` | required (write-limited) | Toggle like. |
| GET | `/poems/:id/likes` | optional | Users who liked. |
| POST | `/poems/:id/comments` | required (write-limited) | Add comment. |
| GET | `/poems/:id/comments` | optional | List comments. |
| DELETE | `/comments/:id` | required | Delete own comment. |
| POST | `/comments/:id/like` | required (write-limited) | Toggle comment like. |
| POST | `/poems/:id/repost` | required (write-limited) | Toggle repost. |
| GET | `/poems/:id/reposters` | optional | Users who reposted. |
| GET | `/users/:id/reposts` | optional | A user's reposts. |

### Feed & search
| Method | Path | Auth | Notes |
|--------|------|------|-------|
| GET | `/feed` | required | Personalized home feed. |
| GET | `/feed/explore` | optional | Explore feed. |
| GET | `/feed/audio` | optional | Audio feed. |
| GET | `/search/poems?q=` | optional | Search poems. |
| GET | `/search/users?q=` | optional | Search users. |

### Reports (bugs / feature requests)
| Method | Path | Auth | Notes |
|--------|------|------|-------|
| POST | `/reports/` | required (write-limited) | Create a report. |
| GET | `/reports/me` | required | My reports. |
| GET | `/reports/` | none | **List all reports (admin view).** |
| PATCH | `/reports/:id` | none | **Update report status (admin/dev).** |

### Moderation reports (content/user reports)
| Method | Path | Auth | Notes |
|--------|------|------|-------|
| POST | `/moderation-reports/` | required (write-limited) | File a moderation report. |
| GET | `/moderation-reports/me` | required | My filed reports. |
| GET | `/moderation-reports/` | none | **List all moderation reports (admin view).** |
| PATCH | `/moderation-reports/:id` | none | **Update moderation report (admin).** |

> **For an admin dashboard**, the most useful no-auth endpoints are:
> `/admin/users`, `/reports/` (+ `PATCH /reports/:id`), and
> `/moderation-reports/` (+ `PATCH /moderation-reports/:id`).

---

## 5. Flutter integration

### 5.1 Dio client with auth + auto-refresh

```dart
import 'package:dio/dio.dart';

class ApiClient {
  ApiClient(this._baseUrl);

  final String _baseUrl;
  String? accessToken;
  String? refreshToken;

  late final Dio dio = Dio(BaseOptions(
    baseUrl: _baseUrl, // e.g. https://renyra-backend.onrender.com/api/v1
    connectTimeout: const Duration(seconds: 15),
    receiveTimeout: const Duration(seconds: 30),
  ))
    ..interceptors.add(InterceptorsWrapper(
      onRequest: (options, handler) {
        if (accessToken != null) {
          options.headers['Authorization'] = 'Bearer $accessToken';
        }
        handler.next(options);
      },
      onError: (e, handler) async {
        // Auto-refresh once on 401
        if (e.response?.statusCode == 401 && refreshToken != null) {
          try {
            final r = await Dio().post(
              '$_baseUrl/auth/refresh',
              data: {'refreshToken': refreshToken},
            );
            accessToken = r.data['data']['accessToken'];
            refreshToken = r.data['data']['refreshToken'];
            final req = e.requestOptions
              ..headers['Authorization'] = 'Bearer $accessToken';
            return handler.resolve(await dio.fetch(req));
          } catch (_) {/* fall through to error */}
        }
        handler.next(e);
      },
    ));
}
```

### 5.2 Generic envelope unwrap

```dart
T unwrap<T>(Response res, T Function(dynamic data) parse) {
  final body = res.data as Map<String, dynamic>;
  if (body['success'] != true) {
    throw Exception(body['message'] ?? 'Request failed');
  }
  return parse(body['data']);
}
```

### 5.3 Model + service for "List all users"

```dart
class AdminUser {
  final String id;
  final String email;
  final String displayName;
  final String? username;
  final String photoURL;
  final bool isActive;
  final bool isBanned;
  final int postsCount;
  final int followersCount;
  final int followingCount;
  final DateTime joinedAt;
  final DateTime? lastLoginAt;

  AdminUser.fromJson(Map<String, dynamic> j)
      : id = j['id'],
        email = j['email'] ?? '',
        displayName = j['displayName'] ?? '',
        username = j['username'],
        photoURL = j['photoURL'] ?? '',
        isActive = j['isActive'] ?? false,
        isBanned = j['isBanned'] ?? false,
        postsCount = j['postsCount'] ?? 0,
        followersCount = j['followersCount'] ?? 0,
        followingCount = j['followingCount'] ?? 0,
        joinedAt = DateTime.parse(j['joinedAt']),
        lastLoginAt = j['lastLoginAt'] != null
            ? DateTime.tryParse(j['lastLoginAt'])
            : null;
}

class UserPage {
  final List<AdminUser> users;
  final int totalCount;
  final bool hasMore;
  UserPage(this.users, this.totalCount, this.hasMore);
}

class AdminService {
  AdminService(this._api);
  final ApiClient _api;

  Future<UserPage> listUsers({
    String q = '',
    int limit = 50,
    int offset = 0,
    String sortBy = 'createdAt',
    String sortDir = 'desc',
  }) async {
    final res = await _api.dio.get('/admin/users', queryParameters: {
      'q': q,
      'limit': limit,
      'offset': offset,
      'sortBy': sortBy,
      'sortDir': sortDir,
    });
    return unwrap(res, (d) => UserPage(
          (d['users'] as List).map((e) => AdminUser.fromJson(e)).toList(),
          d['totalCount'] ?? 0,
          d['hasMore'] ?? false,
        ));
  }
}
```

### 5.4 Login (only if you need protected endpoints)

```dart
Future<void> login(ApiClient api, String firebaseIdToken) async {
  final res = await api.dio.post('/auth/exchange',
      data: {'firebaseToken': firebaseIdToken});
  final data = res.data['data'];
  api.accessToken = data['accessToken'];
  api.refreshToken = data['refreshToken'];
}
```

---

## 6. Quick test with curl

```bash
# List all users (no auth)
curl "http://localhost:8080/api/v1/admin/users?limit=10&sortBy=createdAt&sortDir=desc"

# Search by email
curl "http://localhost:8080/api/v1/admin/users?q=gmail.com&limit=20"

# All reports (admin)
curl "http://localhost:8080/api/v1/reports/"

# Exchange a Firebase token for a JWT
curl -X POST "http://localhost:8080/api/v1/auth/exchange" \
  -H "Content-Type: application/json" \
  -d '{"firebaseToken":"<FIREBASE_ID_TOKEN>"}'
```

---

## 7. Notes for building the admin app

- **Start with the no-auth endpoints** (`/admin/users`, `/reports/`,
  `/moderation-reports/`) — you can build a working dashboard without any login.
- Use server-side pagination (`limit`/`offset` + `hasMore`) for the user list;
  it can be large (thousands of records).
- All timestamps are RFC3339 / ISO-8601 UTC — parse with `DateTime.parse`.
- The `id` field everywhere is a Mongo ObjectID hex string; pass it verbatim to
  `/users/:id/...` style routes.
- For ban/active filtering in the UI, filter client-side on `isBanned` /
  `isActive` (the list endpoint returns all users regardless of status).

---

*Generated for the Renyra admin Flutter app. Endpoint list reflects
`internal/routes/routes.go` as of this commit.*
