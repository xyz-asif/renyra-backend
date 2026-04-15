# Reports & Account Deletion API Documentation

> **Base URL:** `/api/v1`
> **Date:** 2026-04-12
> **Version:** 1.0

---

## Table of Contents

1. [Report Bug / Request Feature](#1-report-bug--request-feature)
2. [Get All Reports (Public)](#2-get-all-reports-public)
3. [Get My Reports](#3-get-my-reports)
4. [Update Report (Admin)](#4-update-report-admin)
5. [Delete Account](#5-delete-account)
6. [Models Reference](#6-models-reference)
7. [Error Codes Reference](#7-error-codes-reference)

---

## 1. Report Bug / Request Feature

Submit a new bug report or feature request.

| | |
|---|---|
| **Method** | `POST` |
| **URL** | `/api/v1/reports` |
| **Auth** | ✅ Required — Bearer Token |
| **Rate Limit** | 10 requests/min (strict) |
| **Content-Type** | `application/json` |

### Request Headers

```
Authorization: Bearer <access_token>
Content-Type: application/json
```

### Request Body

| Field | Type | Required | Description |
|---|---|---|---|
| `isBug` | `boolean` | ✅ Yes | `true` for bug report, `false` for feature request |
| `title` | `string` | ✅ Yes | Short summary (1–200 characters) |
| `description` | `string` | ✅ Yes | Detailed description (1–5000 characters) |
| `imageURL` | `string` | ❌ No | URL of an uploaded screenshot/image |
| `appVersion` | `string` | ❌ No | App version (e.g., `"1.2.0"`) |
| `deviceInfo` | `string` | ❌ No | Device model info (e.g., `"iPhone 15 Pro"`) |
| `platform` | `string` | ❌ No | One of: `"ios"`, `"android"`, `"web"` |

### Request Example

```json
{
  "isBug": true,
  "title": "App crashes when opening chat",
  "description": "Every time I tap on a chat room with more than 50 messages, the app freezes for 3 seconds and then crashes. This started after the latest update.",
  "imageURL": "https://storage.example.com/screenshots/crash-log.png",
  "appVersion": "1.2.0",
  "deviceInfo": "iPhone 15 Pro, iOS 18.1",
  "platform": "ios"
}
```

### Success Response — `201 Created`

```json
{
  "success": true,
  "statusCode": 201,
  "message": "Report submitted successfully",
  "data": {
    "id": "661234abcd567890ef123456",
    "userId": "660abc123def456789abcdef",
    "userName": "Asif",
    "email": "asif@example.com",
    "isBug": true,
    "title": "App crashes when opening chat",
    "description": "Every time I tap on a chat room with more than 50 messages...",
    "imageURL": "https://storage.example.com/screenshots/crash-log.png",
    "status": "open",
    "priority": "medium",
    "appVersion": "1.2.0",
    "deviceInfo": "iPhone 15 Pro, iOS 18.1",
    "platform": "ios",
    "createdAt": "2026-04-12T00:00:00Z",
    "updatedAt": "2026-04-12T00:00:00Z"
  }
}
```

### Error Responses

| Status | Condition | Message |
|---|---|---|
| `401` | Missing/invalid token | `"Unauthorized"` |
| `400` | Malformed JSON body | `"Invalid request body"` |
| `422` | Title empty or > 200 chars | `"title is required and must be between 1 and 200 characters"` |
| `422` | Description empty or > 5000 chars | `"description is required and must be between 1 and 5000 characters"` |
| `422` | Invalid image URL | `"invalid image URL format"` |
| `422` | Invalid platform value | `"platform must be ios, android, or web"` |
| `429` | Rate limit exceeded | `"too many requests, slow down"` |

### Flutter Integration Notes

To automatically capture `appVersion` and `platform`:

```dart
import 'package:package_info_plus/package_info_plus.dart';
import 'dart:io' show Platform;

final packageInfo = await PackageInfo.fromPlatform();

final body = {
  "isBug": true,
  "title": titleController.text,
  "description": descriptionController.text,
  "imageURL": uploadedImageUrl, // nullable — omit if no image
  "appVersion": packageInfo.version,
  "platform": Platform.operatingSystem, // "android" or "ios"
  "deviceInfo": "${Platform.localHostname}", // or use device_info_plus
};
```

---

## 2. Get All Reports (Public)

Retrieve a paginated list of all bug reports and feature requests. **No authentication needed.**

| | |
|---|---|
| **Method** | `GET` |
| **URL** | `/api/v1/reports` |
| **Auth** | ❌ Not required |

### Query Parameters

| Param | Type | Default | Description |
|---|---|---|---|
| `isBug` | `string` | _(none)_ | Filter: `"true"` for bugs only, `"false"` for features only, omit for all |
| `limit` | `int` | `20` | Items per page (max: 100, min: 1) |
| `offset` | `int` | `0` | Number of items to skip |

### Request Examples

```
GET /api/v1/reports                        → All reports, page 1
GET /api/v1/reports?isBug=true             → Only bugs
GET /api/v1/reports?isBug=false            → Only feature requests
GET /api/v1/reports?limit=10&offset=10     → Page 2 (10 items per page)
GET /api/v1/reports?isBug=true&limit=5     → First 5 bugs
```

### Success Response — `200 OK`

```json
{
  "success": true,
  "statusCode": 200,
  "message": "Reports retrieved successfully",
  "data": {
    "reports": [
      {
        "id": "661234abcd567890ef123456",
        "userId": "660abc123def456789abcdef",
        "userName": "Asif",
        "email": "asif@example.com",
        "isBug": true,
        "title": "App crashes when opening chat",
        "description": "Every time I tap on a chat room...",
        "imageURL": "https://storage.example.com/screenshots/crash-log.png",
        "status": "in_progress",
        "priority": "medium",
        "adminReply": "We've identified the issue and are working on a fix.",
        "appVersion": "1.2.0",
        "deviceInfo": "iPhone 15 Pro, iOS 18.1",
        "platform": "ios",
        "createdAt": "2026-04-12T00:00:00Z",
        "updatedAt": "2026-04-12T01:30:00Z"
      },
      {
        "id": "661234abcd567890ef789012",
        "userId": "660abc123def456789aaaaaa",
        "userName": "John",
        "email": "john@example.com",
        "isBug": false,
        "title": "Add dark mode support",
        "description": "It would be great to have a dark mode option...",
        "status": "open",
        "priority": "medium",
        "appVersion": "1.1.0",
        "platform": "android",
        "createdAt": "2026-04-11T10:00:00Z",
        "updatedAt": "2026-04-11T10:00:00Z"
      }
    ],
    "pagination": {
      "limit": 20,
      "offset": 0,
      "hasMore": false
    }
  }
}
```

### Pagination Logic

```
Page 1: offset=0, limit=20
Page 2: offset=20, limit=20
Page 3: offset=40, limit=20
...
Stop when: hasMore == false
```

---

## 3. Get My Reports

Retrieve a paginated list of the **currently authenticated user's** reports. This allows users to track the status of bugs/features they've submitted.

| | |
|---|---|
| **Method** | `GET` |
| **URL** | `/api/v1/reports/me` |
| **Auth** | ✅ Required — Bearer Token |

### Request Headers

```
Authorization: Bearer <access_token>
```

### Query Parameters

| Param | Type | Default | Description |
|---|---|---|---|
| `isBug` | `string` | _(none)_ | Filter: `"true"` for bugs only, `"false"` for feature requests only. If omitted or invalid, returns all. |
| `limit` | `int` | `20` | Items per page (max: 100) |
| `offset` | `int` | `0` | Number of items to skip |

### Request Example

```
GET /api/v1/reports/me?isBug=true&limit=10&offset=0
```

### Success Response — `200 OK`

```json
{
  "success": true,
  "statusCode": 200,
  "message": "User reports retrieved successfully",
  "data": {
    "reports": [
      {
        "id": "661234abcd567890ef123456",
        "userId": "660abc123def456789abcdef",
        "userName": "Asif",
        "email": "asif@example.com",
        "isBug": true,
        "title": "App crashes when opening chat",
        "description": "Every time I tap on a chat room...",
        "status": "resolved",
        "priority": "medium",
        "adminReply": "Fixed in version 1.3.0. Please update your app.",
        "appVersion": "1.2.0",
        "platform": "ios",
        "createdAt": "2026-04-12T00:00:00Z",
        "updatedAt": "2026-04-12T06:00:00Z"
      }
    ],
    "pagination": {
      "limit": 10,
      "offset": 0,
      "hasMore": false
    }
  }
}
```

### Frontend UI Suggestions

Display a status badge for each report:

| Status | Badge Color | Label |
|---|---|---|
| `open` | 🔵 Blue | Open |
| `in_progress` | 🟡 Yellow | In Progress |
| `resolved` | 🟢 Green | Resolved |
| `closed` | ⚫ Grey | Closed |

If `adminReply` is present, show it below the report as a reply card.

---

## 4. Update Report (Admin)

Update the status and/or add an admin reply to a report. This endpoint is **public** (no auth) — intended for admin/developer use only.

| | |
|---|---|
| **Method** | `PATCH` |
| **URL** | `/api/v1/reports/:id` |
| **Auth** | ❌ Not required (admin use) |
| **Content-Type** | `application/json` |

### URL Parameters

| Param | Type | Description |
|---|---|---|
| `id` | `string` | MongoDB ObjectID of the report |

### Request Body

| Field | Type | Required | Description |
|---|---|---|---|
| `status` | `string` | ❌ No | New status. Must be one of: `"open"`, `"in_progress"`, `"resolved"`, `"closed"` |
| `adminReply` | `string` | ❌ No | Reply message from admin visible to the user |

> At least one of `status` or `adminReply` must be provided.

### Request Example

```json
PATCH /api/v1/reports/661234abcd567890ef123456

{
  "status": "resolved",
  "adminReply": "This has been fixed in version 1.3.0. Please update your app from the store."
}
```

### Success Response — `200 OK`

```json
{
  "success": true,
  "statusCode": 200,
  "message": "Report updated successfully",
  "data": {
    "id": "661234abcd567890ef123456",
    "userId": "660abc123def456789abcdef",
    "userName": "Asif",
    "email": "asif@example.com",
    "isBug": true,
    "title": "App crashes when opening chat",
    "description": "Every time I tap on a chat room...",
    "status": "resolved",
    "priority": "medium",
    "adminReply": "This has been fixed in version 1.3.0. Please update your app from the store.",
    "appVersion": "1.2.0",
    "platform": "ios",
    "createdAt": "2026-04-12T00:00:00Z",
    "updatedAt": "2026-04-12T06:00:00Z"
  }
}
```

### Error Responses

| Status | Condition | Message |
|---|---|---|
| `400` | Missing report ID | `"Report ID is required"` |
| `400` | Malformed JSON | `"Invalid request body"` |
| `404` | Report not found | `"report not found"` |
| `422` | Invalid status value | `"invalid status value"` |
| `422` | Invalid report ID format | `"invalid report ID"` |

---

## 5. Delete Account

Permanently delete the authenticated user's account. This is a **hard delete** — the user document is removed from the database. An audit log is saved with the deletion reason for record keeping.

| | |
|---|---|
| **Method** | `DELETE` |
| **URL** | `/api/v1/users/me` |
| **Auth** | ✅ Required — Bearer Token |
| **Content-Type** | `application/json` |

### Request Headers

```
Authorization: Bearer <access_token>
Content-Type: application/json
```

### Request Body

| Field | Type | Required | Description |
|---|---|---|---|
| `reason` | `string` | ✅ Yes | Reason for deleting the account (1–1000 characters) |

### Request Example

```json
DELETE /api/v1/users/me

{
  "reason": "I no longer use the app and want my data removed."
}
```

### Success Response — `200 OK`

```json
{
  "success": true,
  "statusCode": 200,
  "message": "Account deleted successfully"
}
```

### Error Responses

| Status | Condition | Message |
|---|---|---|
| `401` | Missing/invalid token | `"Unauthorized"` |
| `400` | Malformed JSON body | `"invalid request body"` |
| `422` | Empty reason | `"deletion reason is required"` |
| `422` | Reason > 1000 chars | `"deletion reason is too long (max 1000 characters)"` |
| `404` | User not found | `"user not found"` |

### ⚠️ Important Notes for Frontend

1. **This action is irreversible.** Show a confirmation dialog before calling this API.
2. After a successful response, immediately:
   - Clear all local storage / secure storage (tokens, caches)
   - Sign out from Firebase Auth
   - Navigate to the login/welcome screen
3. The user's existing content (poems, comments, etc.) will remain in the database but will show as authored by a deleted/unknown user.

---

## 6. Real-Time Notifications

When an admin updates a report's status or replies to it, the user will receive a notification via WebSocket (or FCM if offline).

To help your frontend navigate the user to either the "Bugs" screen or the "Feature Requests" screen, the API dynamically pushes the `resourceType` based on what the original report was.

| `isBug` | Notification `resourceType` |
|---|---|
| `true` | `"bug_report"` |
| `false` | `"feature_request"` |

**Example Notification Payload received by Frontend:**
```json
{
  "id": "661234abcd567890ef123456",
  "type": "report_admin_reply",
  "resourceType": "bug_report",
  "resourceId": "old-report-id-here",
  "title": "Admin replied to your report",
  "body": "Fixed in version 1.3.0.",
  ...
}
```

### Suggested Confirmation Flow

```
[User taps "Delete Account"]
    ↓
[Show reason text field + warning dialog]
    "Are you sure? This cannot be undone."
    ↓
[User enters reason + confirms]
    ↓
[Call DELETE /api/v1/users/me]
    ↓
[On success: clear tokens → Firebase sign out → navigate to login]
```

---

## 6. Models Reference

### Report Object

```json
{
  "id": "string (ObjectID)",
  "userId": "string (ObjectID)",
  "userName": "string",
  "email": "string",
  "isBug": "boolean",
  "title": "string (1-200 chars)",
  "description": "string (1-5000 chars)",
  "imageURL": "string | omitted if empty",
  "status": "open | in_progress | resolved | closed",
  "priority": "low | medium | high | critical",
  "adminReply": "string | omitted if empty",
  "appVersion": "string | omitted if empty",
  "deviceInfo": "string | omitted if empty",
  "platform": "ios | android | web | omitted if empty",
  "createdAt": "ISO 8601 datetime",
  "updatedAt": "ISO 8601 datetime"
}
```

### Pagination Object

```json
{
  "limit": "int — page size used",
  "offset": "int — items skipped",
  "hasMore": "boolean — true if more pages exist"
}
```

---

## 7. Error Codes Reference

All error responses follow the standard API response format:

```json
{
  "success": false,
  "statusCode": 422,
  "message": "description of what went wrong"
}
```

| Code | Meaning |
|---|---|
| `400` | Bad Request — malformed JSON or missing required URL params |
| `401` | Unauthorized — missing or invalid Bearer token |
| `404` | Not Found — the requested resource doesn't exist |
| `422` | Validation Failed — input data doesn't meet requirements |
| `429` | Too Many Requests — rate limit exceeded |
| `500` | Internal Server Error — unexpected server failure |

---

## Summary of New Endpoints

| Method | Endpoint | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/reports` | ✅ Yes | Submit a bug report or feature request |
| `GET` | `/api/v1/reports` | ❌ No | List all reports (paginated + filterable) |
| `GET` | `/api/v1/reports/me` | ✅ Yes | List my submitted reports |
| `PATCH` | `/api/v1/reports/:id` | ❌ No | Update report status / admin reply |
| `DELETE` | `/api/v1/users/me` | ✅ Yes | Permanently delete user account |
