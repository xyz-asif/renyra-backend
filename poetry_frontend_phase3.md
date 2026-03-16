# ChatBee Poetry App — Frontend Phase 3
### Flutter + Riverpod — Extends Existing Codebase

---

## Overview

This document covers:
1. Poem like button (on PoemCard + PoemDetailScreen)
2. Comment bottom sheet on PoemDetailScreen (flat list, add, delete, like comments, @mention input)
3. Repost button (on PoemCard + PoemDetailScreen)
4. Self profile screen (own profile with Poems / Drafts / Reposts tabs)
5. Repost card widget (Twitter style — shows reposter + full original poem)
6. Followers list screen
7. Following list screen
8. Notification screen UI

All builds on existing code. Do not change any chat, WebSocket, or feed controller logic.

---

## Part 1 — New API Endpoints

### File: `lib/core/constants/api_endpoints.dart`

Add:

```dart
// Social
static String poemLike(String id) => '/poems/$id/like';
static String poemLikes(String id) => '/poems/$id/likes';
static String poemComments(String id) => '/poems/$id/comments';
static String commentDelete(String id) => '/comments/$id';
static String commentLike(String id) => '/comments/$id/like';
static String poemRepost(String id) => '/poems/$id/repost';
static String userReposts(String userId) => '/users/$userId/reposts';
```

---

## Part 2 — New Models

### File: `lib/features/social/models/comment_model.dart`

```dart
import 'package:chatbee/features/poems/models/poem_model.dart';

class CommentModel {
  final String id;
  final String poemId;
  final PoemAuthor author;
  final String content;
  final int likesCount;
  final bool isLikedByMe;
  final bool isDeleted;
  final DateTime? createdAt;

  const CommentModel({
    required this.id,
    required this.poemId,
    required this.author,
    required this.content,
    this.likesCount = 0,
    this.isLikedByMe = false,
    this.isDeleted = false,
    this.createdAt,
  });

  factory CommentModel.fromJson(Map<String, dynamic> json) {
    return CommentModel(
      id: json['id'] as String? ?? '',
      poemId: json['poemId'] as String? ?? '',
      author: PoemAuthor.fromJson(json['author'] as Map<String, dynamic>? ?? {}),
      content: json['content'] as String? ?? '',
      likesCount: json['likesCount'] as int? ?? 0,
      isLikedByMe: json['isLikedByMe'] as bool? ?? false,
      isDeleted: json['isDeleted'] as bool? ?? false,
      createdAt: json['createdAt'] != null ? DateTime.tryParse(json['createdAt'] as String) : null,
    );
  }

  CommentModel copyWith({bool? isLikedByMe, int? likesCount}) {
    return CommentModel(
      id: id, poemId: poemId, author: author, content: content,
      isDeleted: isDeleted, createdAt: createdAt,
      likesCount: likesCount ?? this.likesCount,
      isLikedByMe: isLikedByMe ?? this.isLikedByMe,
    );
  }
}

class CommentsPage {
  final List<CommentModel> comments;
  final bool hasMore;

  const CommentsPage({required this.comments, required this.hasMore});

  factory CommentsPage.fromJson(Map<String, dynamic> json) {
    final list = json['comments'] as List? ?? [];
    return CommentsPage(
      comments: list.map((e) => CommentModel.fromJson(e as Map<String, dynamic>)).toList(),
      hasMore: json['hasMore'] as bool? ?? false,
    );
  }
}
```

### Update `lib/features/poems/models/poem_model.dart`

Add these fields to `PoemModel`:

```dart
// Add to PoemModel fields:
@JsonKey(defaultValue: false)
final bool isLikedByMe;
@JsonKey(defaultValue: false)
final bool isRepostedByMe;
@JsonKey(defaultValue: false)
final bool isRepost;
final PoemModel? originalPoem; // non-null when isRepost == true
```

Add to constructor, fromJson, copyWith accordingly. For `originalPoem`, use a custom fromJson:
```dart
originalPoem: json['originalPoem'] != null
    ? PoemModel.fromJson(json['originalPoem'] as Map<String, dynamic>)
    : null,
```

Run `build_runner` after this change.

---

## Part 3 — Social Repo

### File: `lib/features/social/repos/social_repo.dart`

```dart
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:riverpod_annotation/riverpod_annotation.dart';
import 'package:chatbee/core/constants/api_endpoints.dart';
import 'package:chatbee/core/network/api_client.dart';
import 'package:chatbee/features/social/models/comment_model.dart';
import 'package:chatbee/features/poems/models/poem_model.dart';

part 'social_repo.g.dart';

class LikeResult {
  final bool liked;
  final int likesCount;
  LikeResult({required this.liked, required this.likesCount});
  factory LikeResult.fromJson(Map<String, dynamic> json) => LikeResult(
    liked: json['liked'] as bool? ?? false,
    likesCount: json['likesCount'] as int? ?? 0,
  );
}

class RepostResult {
  final bool reposted;
  final int repostsCount;
  RepostResult({required this.reposted, required this.repostsCount});
  factory RepostResult.fromJson(Map<String, dynamic> json) => RepostResult(
    reposted: json['reposted'] as bool? ?? false,
    repostsCount: json['repostsCount'] as int? ?? 0,
  );
}

class SocialRepo {
  final ApiClient apiClient;
  SocialRepo({required this.apiClient});

  Future<LikeResult> togglePoemLike(String poemId) async {
    final response = await apiClient.post(ApiEndpoints.poemLike(poemId));
    return LikeResult.fromJson(response.data as Map<String, dynamic>);
  }

  Future<CommentsPage> getComments(String poemId, {int limit = 20, String? before}) async {
    final query = <String, dynamic>{'limit': limit};
    if (before != null) query['before'] = before;
    final response = await apiClient.get(ApiEndpoints.poemComments(poemId), queryParameters: query);
    return CommentsPage.fromJson(response.data as Map<String, dynamic>);
  }

  Future<CommentModel> addComment(String poemId, String content) async {
    final response = await apiClient.post(
      ApiEndpoints.poemComments(poemId),
      data: {'content': content},
    );
    return CommentModel.fromJson(response.data as Map<String, dynamic>);
  }

  Future<void> deleteComment(String commentId) async {
    await apiClient.delete(ApiEndpoints.commentDelete(commentId));
  }

  Future<LikeResult> toggleCommentLike(String commentId) async {
    final response = await apiClient.post(ApiEndpoints.commentLike(commentId));
    return LikeResult.fromJson(response.data as Map<String, dynamic>);
  }

  Future<RepostResult> toggleRepost(String poemId) async {
    final response = await apiClient.post(ApiEndpoints.poemRepost(poemId));
    return RepostResult.fromJson(response.data as Map<String, dynamic>);
  }

  Future<PoemsPage> getUserReposts(String userId, {int limit = 20, String? before}) async {
    final query = <String, dynamic>{'limit': limit};
    if (before != null) query['before'] = before;
    final response = await apiClient.get(ApiEndpoints.userReposts(userId), queryParameters: query);
    return PoemsPage.fromJson(response.data as Map<String, dynamic>);
  }
}

@riverpod
SocialRepo socialRepo(Ref ref) {
  return SocialRepo(apiClient: ref.read(apiClientProvider));
}
```

---

## Part 4 — Update PoemCard Widget

### File: `lib/features/poems/widgets/poem_card.dart`

Make the following changes to the existing `PoemCard` widget. Do not rewrite it — only modify what is listed.

**1. Make PoemCard stateful** (it needs to hold optimistic like/repost state):

Change `class PoemCard extends StatelessWidget` to `class PoemCard extends ConsumerStatefulWidget` and add state:

```dart
class _PoemCardState extends ConsumerState<PoemCard> {
  late bool _isLiked;
  late int _likeCount;
  late bool _isReposted;
  late int _repostCount;
  bool _isLikeLoading = false;
  bool _isRepostLoading = false;

  @override
  void initState() {
    super.initState();
    _isLiked = widget.poem.isLikedByMe;
    _likeCount = widget.poem.likesCount;
    _isReposted = widget.poem.isRepostedByMe;
    _repostCount = widget.poem.repostsCount;
  }
```

**2. Replace the footer Row** (where `likesCount` and `commentsCount` are shown) with this:

```dart
Row(
  children: [
    // Like button
    GestureDetector(
      onTap: _isLikeLoading ? null : _toggleLike,
      child: Row(
        children: [
          Icon(
            _isLiked ? Icons.favorite_rounded : Icons.favorite_border_rounded,
            size: 18.r,
            color: _isLiked ? Colors.red : AppTheme.textLightColor,
          ),
          SizedBox(width: 4.w),
          Text('$_likeCount', style: TextStyle(fontSize: 13.sp, color: AppTheme.textLightColor)),
        ],
      ),
    ),

    SizedBox(width: 16.w),

    // Comment button — opens comment sheet
    GestureDetector(
      onTap: () => _showComments(context),
      child: Row(
        children: [
          Icon(Icons.chat_bubble_outline_rounded, size: 18.r, color: AppTheme.textLightColor),
          SizedBox(width: 4.w),
          Text('${widget.poem.commentsCount}', style: TextStyle(fontSize: 13.sp, color: AppTheme.textLightColor)),
        ],
      ),
    ),

    SizedBox(width: 16.w),

    // Repost button
    GestureDetector(
      onTap: _isRepostLoading ? null : _toggleRepost,
      child: Row(
        children: [
          Icon(
            Icons.repeat_rounded,
            size: 18.r,
            color: _isReposted ? AppTheme.primaryColor : AppTheme.textLightColor,
          ),
          SizedBox(width: 4.w),
          Text('$_repostCount',
              style: TextStyle(
                fontSize: 13.sp,
                color: _isReposted ? AppTheme.primaryColor : AppTheme.textLightColor,
              )),
        ],
      ),
    ),

    const Spacer(),

    if (widget.poem.isOriginal)
      Icon(Icons.copyright_rounded, size: 14.r, color: AppTheme.primaryColor),
    if (widget.poem.hasAudio) ...[
      SizedBox(width: 6.w),
      Icon(Icons.mic_rounded, size: 14.r, color: AppTheme.textLightColor),
    ],
  ],
),
```

**3. Add these methods to `_PoemCardState`:**

```dart
Future<void> _toggleLike() async {
  // Optimistic update
  setState(() {
    _isLikeLoading = true;
    _isLiked = !_isLiked;
    _likeCount += _isLiked ? 1 : -1;
  });
  try {
    final result = await ref.read(socialRepoProvider).togglePoemLike(widget.poem.id);
    if (mounted) setState(() { _isLiked = result.liked; _likeCount = result.likesCount; });
  } catch (_) {
    // Revert on failure
    if (mounted) setState(() { _isLiked = !_isLiked; _likeCount += _isLiked ? 1 : -1; });
  } finally {
    if (mounted) setState(() => _isLikeLoading = false);
  }
}

Future<void> _toggleRepost() async {
  setState(() { _isRepostLoading = true; });
  try {
    final result = await ref.read(socialRepoProvider).toggleRepost(widget.poem.id);
    if (mounted) setState(() {
      _isReposted = result.reposted;
      _repostCount = result.repostsCount;
    });
  } catch (e) {
    if (mounted) AppSnackbar.show(context, message: e.toString(), type: SnackbarType.error);
  } finally {
    if (mounted) setState(() => _isRepostLoading = false);
  }
}

void _showComments(BuildContext context) {
  showModalBottomSheet(
    context: context,
    isScrollControlled: true,
    backgroundColor: Colors.transparent,
    builder: (_) => CommentBottomSheet(poemId: widget.poem.id),
  );
}
```

---

## Part 5 — Repost Card Widget

### File: `lib/features/poems/widgets/repost_card.dart`

This widget is used in the Reposts tab of the profile screen. It shows the reposter at the top and the full original poem card below — exactly like Twitter.

```dart
import 'package:flutter/material.dart';
import 'package:flutter_screenutil/flutter_screenutil.dart';
import 'package:go_router/go_router.dart';
import 'package:cached_network_image/cached_network_image.dart';
import 'package:timeago/timeago.dart' as timeago;
import 'package:chatbee/config/theme/app_theme.dart';
import 'package:chatbee/features/poems/models/poem_model.dart';
import 'package:chatbee/features/poems/widgets/poem_card.dart';

class RepostCard extends StatelessWidget {
  final PoemModel repost; // isRepost == true, has originalPoem

  const RepostCard({super.key, required this.repost});

  @override
  Widget build(BuildContext context) {
    final original = repost.originalPoem;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // ── Reposter header ──
        Padding(
          padding: EdgeInsets.fromLTRB(16.w, 10.h, 16.w, 4.h),
          child: GestureDetector(
            onTap: () => context.push('/profile/${repost.author.id}'),
            child: Row(
              children: [
                Icon(Icons.repeat_rounded, size: 14.r, color: AppTheme.textLightColor),
                SizedBox(width: 6.w),
                CircleAvatar(
                  radius: 12.r,
                  backgroundColor: AppTheme.borderColor,
                  backgroundImage: repost.author.photoURL.isNotEmpty
                      ? CachedNetworkImageProvider(repost.author.photoURL)
                      : null,
                ),
                SizedBox(width: 6.w),
                Text(
                  '${repost.author.displayName} reposted',
                  style: TextStyle(fontSize: 13.sp, color: AppTheme.textLightColor),
                ),
                const Spacer(),
                if (repost.createdAt != null)
                  Text(
                    timeago.format(repost.createdAt!, locale: 'en_short'),
                    style: TextStyle(fontSize: 11.sp, color: AppTheme.textLightColor),
                  ),
              ],
            ),
          ),
        ),

        // ── Original poem card (full, same as feed) ──
        if (original != null)
          PoemCard(poem: original)
        else
          Padding(
            padding: EdgeInsets.all(16.w),
            child: Text(
              'Original poem unavailable',
              style: TextStyle(fontSize: 13.sp, color: AppTheme.textLightColor, fontStyle: FontStyle.italic),
            ),
          ),

        Divider(height: 1, color: AppTheme.borderColor),
      ],
    );
  }
}
```

---

## Part 6 — Comment Bottom Sheet

### File: `lib/features/social/widgets/comment_bottom_sheet.dart`

```dart
import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_screenutil/flutter_screenutil.dart';
import 'package:cached_network_image/cached_network_image.dart';
import 'package:timeago/timeago.dart' as timeago;
import 'package:chatbee/config/theme/app_theme.dart';
import 'package:chatbee/features/auth/controllers/auth_controller.dart';
import 'package:chatbee/features/social/models/comment_model.dart';
import 'package:chatbee/features/social/repos/social_repo.dart';
import 'package:chatbee/shared/widgets/app_snackbar.dart';

class CommentBottomSheet extends ConsumerStatefulWidget {
  final String poemId;

  const CommentBottomSheet({super.key, required this.poemId});

  @override
  ConsumerState<CommentBottomSheet> createState() => _CommentBottomSheetState();
}

class _CommentBottomSheetState extends ConsumerState<CommentBottomSheet> {
  final TextEditingController _inputController = TextEditingController();
  final ScrollController _scrollController = ScrollController();
  final FocusNode _inputFocusNode = FocusNode();

  List<CommentModel> _comments = [];
  bool _isLoading = true;
  bool _isSending = false;
  bool _hasMore = false;
  String? _before;

  // @mention suggestion state
  List<String> _mentionSuggestions = [];
  String _currentMentionQuery = '';
  Timer? _mentionDebounce;

  @override
  void initState() {
    super.initState();
    _loadComments();
    _inputController.addListener(_onInputChanged);
  }

  @override
  void dispose() {
    _inputController.dispose();
    _scrollController.dispose();
    _inputFocusNode.dispose();
    _mentionDebounce?.cancel();
    super.dispose();
  }

  Future<void> _loadComments() async {
    try {
      final page = await ref.read(socialRepoProvider).getComments(widget.poemId, limit: 20);
      if (mounted) {
        setState(() {
          _comments = page.comments;
          _hasMore = page.hasMore;
          _isLoading = false;
          if (page.comments.isNotEmpty) _before = page.comments.last.id;
        });
      }
    } catch (_) {
      if (mounted) setState(() => _isLoading = false);
    }
  }

  Future<void> _sendComment() async {
    final content = _inputController.text.trim();
    if (content.isEmpty || _isSending) return;

    setState(() => _isSending = true);
    try {
      final comment = await ref.read(socialRepoProvider).addComment(widget.poemId, content);
      _inputController.clear();
      if (mounted) {
        setState(() {
          _comments.insert(0, comment); // newest at top
          _mentionSuggestions = [];
        });
      }
    } catch (e) {
      if (mounted) AppSnackbar.show(context, message: 'Failed to post comment', type: SnackbarType.error);
    } finally {
      if (mounted) setState(() => _isSending = false);
    }
  }

  Future<void> _deleteComment(CommentModel comment) async {
    try {
      await ref.read(socialRepoProvider).deleteComment(comment.id);
      if (mounted) setState(() => _comments.removeWhere((c) => c.id == comment.id));
    } catch (_) {}
  }

  Future<void> _toggleCommentLike(CommentModel comment) async {
    final index = _comments.indexWhere((c) => c.id == comment.id);
    if (index < 0) return;

    // Optimistic update
    final wasLiked = comment.isLikedByMe;
    setState(() {
      _comments[index] = comment.copyWith(
        isLikedByMe: !wasLiked,
        likesCount: wasLiked ? comment.likesCount - 1 : comment.likesCount + 1,
      );
    });

    try {
      final result = await ref.read(socialRepoProvider).toggleCommentLike(comment.id);
      if (mounted) {
        setState(() {
          _comments[index] = _comments[index].copyWith(
            isLikedByMe: result.liked,
            likesCount: result.likesCount,
          );
        });
      }
    } catch (_) {
      // Revert
      if (mounted) setState(() { _comments[index] = comment; });
    }
  }

  // Detect @mention as user types
  void _onInputChanged() {
    final text = _inputController.text;
    final cursor = _inputController.selection.baseOffset;
    if (cursor < 0) return;

    final textBeforeCursor = text.substring(0, cursor);
    final atIndex = textBeforeCursor.lastIndexOf('@');

    if (atIndex >= 0) {
      final query = textBeforeCursor.substring(atIndex + 1);
      // Only show suggestions if no space after @
      if (!query.contains(' ') && query.isNotEmpty) {
        _currentMentionQuery = query;
        _mentionDebounce?.cancel();
        _mentionDebounce = Timer(const Duration(milliseconds: 300), () => _fetchMentionSuggestions(query));
        return;
      }
    }

    if (_mentionSuggestions.isNotEmpty) {
      setState(() { _mentionSuggestions = []; _currentMentionQuery = ''; });
    }
  }

  Future<void> _fetchMentionSuggestions(String query) async {
    // Reuse the existing user search endpoint
    try {
      final response = await ref.read(socialRepoProvider).searchUsersForMention(query);
      if (mounted && _currentMentionQuery == query) {
        setState(() => _mentionSuggestions = response);
      }
    } catch (_) {}
  }

  void _insertMention(String username) {
    final text = _inputController.text;
    final cursor = _inputController.selection.baseOffset;
    final textBeforeCursor = text.substring(0, cursor);
    final atIndex = textBeforeCursor.lastIndexOf('@');

    if (atIndex >= 0) {
      final newText = text.substring(0, atIndex) + '@$username ' + text.substring(cursor);
      _inputController.value = TextEditingValue(
        text: newText,
        selection: TextSelection.collapsed(offset: atIndex + username.length + 2),
      );
    }
    setState(() { _mentionSuggestions = []; _currentMentionQuery = ''; });
  }

  @override
  Widget build(BuildContext context) {
    final currentUserId = ref.read(authControllerProvider).valueOrNull?.id;

    return DraggableScrollableSheet(
      initialChildSize: 0.75,
      maxChildSize: 0.95,
      minChildSize: 0.4,
      expand: false,
      builder: (_, scrollController) {
        return Container(
          decoration: BoxDecoration(
            color: AppTheme.surfaceColor,
            borderRadius: BorderRadius.vertical(top: Radius.circular(20.r)),
          ),
          child: SafeArea(
            top: false,
            child: Column(
              children: [
                // Drag handle
                Padding(
                  padding: EdgeInsets.only(top: 12.h, bottom: 8.h),
                  child: Center(
                    child: Container(
                      width: 40.w, height: 4.h,
                      decoration: BoxDecoration(color: AppTheme.borderColor, borderRadius: BorderRadius.circular(2.r)),
                    ),
                  ),
                ),

                Padding(
                  padding: EdgeInsets.symmetric(horizontal: 20.w, vertical: 4.h),
                  child: Align(
                    alignment: Alignment.centerLeft,
                    child: Text('Comments', style: TextStyle(fontSize: 17.sp, fontWeight: FontWeight.w700, color: AppTheme.textDarkColor)),
                  ),
                ),

                Divider(height: 1, color: AppTheme.borderColor),

                // ── Comment list ──
                Flexible(
                  child: _isLoading
                      ? const Center(child: CircularProgressIndicator())
                      : _comments.isEmpty
                          ? Center(
                              child: Text('No comments yet. Be the first!',
                                  style: TextStyle(fontSize: 14.sp, color: AppTheme.textMediumColor)))
                          : ListView.separated(
                              controller: scrollController,
                              padding: EdgeInsets.symmetric(horizontal: 16.w, vertical: 8.h),
                              itemCount: _comments.length,
                              separatorBuilder: (_, __) => Divider(height: 1, color: AppTheme.borderColor.withValues(alpha: 0.5)),
                              itemBuilder: (_, i) {
                                final comment = _comments[i];
                                final isOwn = comment.author.id == currentUserId;

                                return Padding(
                                  padding: EdgeInsets.symmetric(vertical: 10.h),
                                  child: Row(
                                    crossAxisAlignment: CrossAxisAlignment.start,
                                    children: [
                                      CircleAvatar(
                                        radius: 16.r,
                                        backgroundColor: AppTheme.borderColor,
                                        backgroundImage: comment.author.photoURL.isNotEmpty
                                            ? CachedNetworkImageProvider(comment.author.photoURL)
                                            : null,
                                      ),
                                      SizedBox(width: 10.w),
                                      Expanded(
                                        child: Column(
                                          crossAxisAlignment: CrossAxisAlignment.start,
                                          children: [
                                            Row(
                                              children: [
                                                Text(comment.author.displayName,
                                                    style: TextStyle(fontSize: 13.sp, fontWeight: FontWeight.w600, color: AppTheme.textDarkColor)),
                                                if (comment.createdAt != null) ...[
                                                  SizedBox(width: 6.w),
                                                  Text(
                                                    timeago.format(comment.createdAt!, locale: 'en_short'),
                                                    style: TextStyle(fontSize: 11.sp, color: AppTheme.textLightColor),
                                                  ),
                                                ],
                                              ],
                                            ),
                                            SizedBox(height: 4.h),
                                            Text(
                                              comment.content,
                                              style: TextStyle(fontSize: 14.sp, color: AppTheme.textMediumColor, height: 1.4),
                                            ),
                                            SizedBox(height: 6.h),
                                            Row(
                                              children: [
                                                // Comment like button
                                                GestureDetector(
                                                  onTap: () => _toggleCommentLike(comment),
                                                  child: Row(
                                                    children: [
                                                      Icon(
                                                        comment.isLikedByMe ? Icons.favorite_rounded : Icons.favorite_border_rounded,
                                                        size: 14.r,
                                                        color: comment.isLikedByMe ? Colors.red : AppTheme.textLightColor,
                                                      ),
                                                      SizedBox(width: 3.w),
                                                      if (comment.likesCount > 0)
                                                        Text('${comment.likesCount}',
                                                            style: TextStyle(fontSize: 12.sp, color: AppTheme.textLightColor)),
                                                    ],
                                                  ),
                                                ),
                                                const Spacer(),
                                                // Delete button — own comments only
                                                if (isOwn)
                                                  GestureDetector(
                                                    onTap: () => _deleteComment(comment),
                                                    child: Text('Delete',
                                                        style: TextStyle(fontSize: 12.sp, color: Colors.red.withValues(alpha: 0.7))),
                                                  ),
                                              ],
                                            ),
                                          ],
                                        ),
                                      ),
                                    ],
                                  ),
                                );
                              },
                            ),
                ),

                // ── Mention suggestions ──
                if (_mentionSuggestions.isNotEmpty)
                  Container(
                    constraints: BoxConstraints(maxHeight: 160.h),
                    decoration: BoxDecoration(
                      color: AppTheme.surfaceColor,
                      border: Border(top: BorderSide(color: AppTheme.borderColor)),
                    ),
                    child: ListView.builder(
                      shrinkWrap: true,
                      itemCount: _mentionSuggestions.length,
                      itemBuilder: (_, i) => ListTile(
                        dense: true,
                        title: Text('@${_mentionSuggestions[i]}',
                            style: TextStyle(fontSize: 14.sp, color: AppTheme.textDarkColor)),
                        onTap: () => _insertMention(_mentionSuggestions[i]),
                      ),
                    ),
                  ),

                // ── Input bar ──
                Container(
                  padding: EdgeInsets.fromLTRB(16.w, 8.h, 16.w, MediaQuery.of(context).viewInsets.bottom + 12.h),
                  decoration: BoxDecoration(
                    color: AppTheme.surfaceColor,
                    border: Border(top: BorderSide(color: AppTheme.borderColor)),
                  ),
                  child: Row(
                    children: [
                      Expanded(
                        child: TextField(
                          controller: _inputController,
                          focusNode: _inputFocusNode,
                          maxLines: 3,
                          minLines: 1,
                          style: TextStyle(fontSize: 14.sp, color: AppTheme.textDarkColor),
                          decoration: InputDecoration(
                            hintText: 'Add a comment... use @username to mention',
                            hintStyle: TextStyle(fontSize: 13.sp, color: AppTheme.textLightColor),
                            filled: true,
                            fillColor: AppTheme.featureBackgroundColor,
                            border: OutlineInputBorder(
                              borderRadius: BorderRadius.circular(20.r),
                              borderSide: BorderSide.none,
                            ),
                            contentPadding: EdgeInsets.symmetric(horizontal: 14.w, vertical: 10.h),
                          ),
                        ),
                      ),
                      SizedBox(width: 8.w),
                      GestureDetector(
                        onTap: _isSending ? null : _sendComment,
                        child: Container(
                          width: 40.r, height: 40.r,
                          decoration: const BoxDecoration(color: AppTheme.primaryColor, shape: BoxShape.circle),
                          child: _isSending
                              ? Padding(
                                  padding: EdgeInsets.all(10.r),
                                  child: const CircularProgressIndicator(color: Colors.white, strokeWidth: 2),
                                )
                              : Icon(Icons.send_rounded, color: Colors.white, size: 18.r),
                        ),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        );
      },
    );
  }
}
```

Also add this method to `SocialRepo` for mention search:

```dart
// Add to SocialRepo in social_repo.dart:
Future<List<String>> searchUsersForMention(String query) async {
  final response = await apiClient.get(
    ApiEndpoints.searchUsers,
    queryParameters: {'q': query, 'limit': 5},
  );
  final data = response.data as Map<String, dynamic>;
  final users = data['users'] as List? ?? [];
  return users
      .map((u) => (u as Map<String, dynamic>)['username'] as String? ?? '')
      .where((u) => u.isNotEmpty)
      .toList();
}
```

---

## Part 7 — Self Profile Screen

### File: `lib/features/profile/screens/self_profile_screen.dart`

```dart
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_screenutil/flutter_screenutil.dart';
import 'package:go_router/go_router.dart';
import 'package:cached_network_image/cached_network_image.dart';
import 'package:chatbee/config/theme/app_theme.dart';
import 'package:chatbee/features/auth/controllers/auth_controller.dart';
import 'package:chatbee/features/poems/controllers/poem_controller.dart';
import 'package:chatbee/features/poems/models/poem_model.dart';
import 'package:chatbee/features/poems/widgets/poem_card.dart';
import 'package:chatbee/features/poems/widgets/repost_card.dart';
import 'package:chatbee/features/social/repos/social_repo.dart';

class SelfProfileScreen extends ConsumerStatefulWidget {
  const SelfProfileScreen({super.key});

  @override
  ConsumerState<SelfProfileScreen> createState() => _SelfProfileScreenState();
}

class _SelfProfileScreenState extends ConsumerState<SelfProfileScreen>
    with SingleTickerProviderStateMixin {
  late TabController _tabController;

  // Reposts loaded separately (not in MyPoemsController)
  List<PoemModel> _reposts = [];
  bool _isLoadingReposts = true;

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 3, vsync: this);
    _tabController.addListener(() {
      if (_tabController.index == 2 && _isLoadingReposts) {
        _loadReposts();
      }
    });
  }

  @override
  void dispose() {
    _tabController.dispose();
    super.dispose();
  }

  Future<void> _loadReposts() async {
    final user = ref.read(authControllerProvider).valueOrNull;
    if (user == null) return;
    try {
      final page = await ref.read(socialRepoProvider).getUserReposts(user.id);
      if (mounted) setState(() { _reposts = page.poems; _isLoadingReposts = false; });
    } catch (_) {
      if (mounted) setState(() => _isLoadingReposts = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final user = ref.watch(authControllerProvider).valueOrNull;
    final myPoemsState = ref.watch(myPoemsControllerProvider);

    final allPoems = myPoemsState.valueOrNull ?? [];
    final publicPoems = allPoems.where((p) => p.isPublic && !p.isRepost).toList();
    final drafts = allPoems.where((p) => p.isDraft).toList();

    return Scaffold(
      body: NestedScrollView(
        headerSliverBuilder: (_, __) => [
          SliverToBoxAdapter(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                // ── Cover image ──
                Stack(
                  children: [
                    Container(
                      height: 160.h,
                      width: double.infinity,
                      color: AppTheme.featureBackgroundColor,
                      child: (user?.coverImageURL?.isNotEmpty == true)
                          ? CachedNetworkImage(imageUrl: user!.coverImageURL!, fit: BoxFit.cover)
                          : null,
                    ),
                    // Edit button top-right
                    Positioned(
                      top: 44.h, right: 12.w,
                      child: GestureDetector(
                        onTap: () => context.push('/profile/edit'),
                        child: Container(
                          padding: EdgeInsets.symmetric(horizontal: 14.w, vertical: 6.h),
                          decoration: BoxDecoration(
                            color: Colors.black45,
                            borderRadius: BorderRadius.circular(20.r),
                            border: Border.all(color: Colors.white24),
                          ),
                          child: Text('Edit profile',
                              style: TextStyle(fontSize: 13.sp, color: Colors.white, fontWeight: FontWeight.w500)),
                        ),
                      ),
                    ),
                  ],
                ),

                Padding(
                  padding: EdgeInsets.symmetric(horizontal: 16.w),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Transform.translate(
                        offset: Offset(0, -32.h),
                        child: CircleAvatar(
                          radius: 44.r,
                          backgroundColor: AppTheme.borderColor,
                          backgroundImage: (user?.photoURL?.isNotEmpty == true)
                              ? CachedNetworkImageProvider(user!.photoURL!) as ImageProvider
                              : null,
                          child: (user?.photoURL == null || user!.photoURL!.isEmpty)
                              ? Text(user?.displayName.isNotEmpty == true ? user!.displayName[0].toUpperCase() : '?',
                                  style: TextStyle(fontSize: 28.sp, color: Colors.white))
                              : null,
                        ),
                      ),

                      Transform.translate(
                        offset: Offset(0, -20.h),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(user?.displayName ?? '',
                                style: TextStyle(fontSize: 20.sp, fontWeight: FontWeight.w700, color: AppTheme.textDarkColor)),
                            Text('@${user?.username ?? ''}',
                                style: TextStyle(fontSize: 14.sp, color: AppTheme.textLightColor)),
                            if (user?.bio?.isNotEmpty == true) ...[
                              SizedBox(height: 6.h),
                              Text(user!.bio!, style: TextStyle(fontSize: 14.sp, color: AppTheme.textMediumColor, height: 1.4)),
                            ],
                            SizedBox(height: 12.h),

                            // Stats row — tappable follower/following
                            Row(
                              children: [
                                _StatItem(label: 'Poems', value: user?.postsCount ?? 0),
                                SizedBox(width: 20.w),
                                GestureDetector(
                                  onTap: () => context.push('/profile/${user?.id}/followers'),
                                  child: _StatItem(label: 'Followers', value: user?.followersCount ?? 0),
                                ),
                                SizedBox(width: 20.w),
                                GestureDetector(
                                  onTap: () => context.push('/profile/${user?.id}/following'),
                                  child: _StatItem(label: 'Following', value: user?.followingCount ?? 0),
                                ),
                              ],
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),

          // ── Tab bar ──
          SliverPersistentHeader(
            pinned: true,
            delegate: _TabBarDelegate(
              TabBar(
                controller: _tabController,
                labelColor: AppTheme.primaryColor,
                unselectedLabelColor: AppTheme.textMediumColor,
                indicatorColor: AppTheme.primaryColor,
                tabs: const [Tab(text: 'Poems'), Tab(text: 'Drafts'), Tab(text: 'Reposts')],
              ),
            ),
          ),
        ],
        body: TabBarView(
          controller: _tabController,
          children: [
            // ── Poems tab ──
            publicPoems.isEmpty
                ? Center(child: Text('No public poems yet', style: TextStyle(fontSize: 15.sp, color: AppTheme.textMediumColor)))
                : ListView.builder(
                    itemCount: publicPoems.length,
                    itemBuilder: (_, i) => PoemCard(
                      poem: publicPoems[i],
                      onTap: () => context.push('/editor', extra: publicPoems[i]),
                    ),
                  ),

            // ── Drafts tab ──
            drafts.isEmpty
                ? Center(child: Text('No drafts', style: TextStyle(fontSize: 15.sp, color: AppTheme.textMediumColor)))
                : ListView.builder(
                    itemCount: drafts.length,
                    itemBuilder: (_, i) => PoemCard(
                      poem: drafts[i],
                      onTap: () => context.push('/editor', extra: drafts[i]),
                    ),
                  ),

            // ── Reposts tab ──
            _isLoadingReposts
                ? const Center(child: CircularProgressIndicator())
                : _reposts.isEmpty
                    ? Center(child: Text('No reposts yet', style: TextStyle(fontSize: 15.sp, color: AppTheme.textMediumColor)))
                    : ListView.builder(
                        itemCount: _reposts.length,
                        itemBuilder: (_, i) => RepostCard(repost: _reposts[i]),
                      ),
          ],
        ),
      ),

      // FAB — create new poem
      floatingActionButton: FloatingActionButton(
        onPressed: () => context.push('/editor'),
        backgroundColor: AppTheme.primaryColor,
        child: Icon(Icons.edit_rounded, color: Colors.white, size: 22.r),
      ),
    );
  }
}

class _StatItem extends StatelessWidget {
  final String label;
  final int value;
  const _StatItem({required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        Text('$value', style: TextStyle(fontSize: 16.sp, fontWeight: FontWeight.w700, color: AppTheme.textDarkColor)),
        Text(label, style: TextStyle(fontSize: 12.sp, color: AppTheme.textLightColor)),
      ],
    );
  }
}

// Needed to pin the TabBar in NestedScrollView
class _TabBarDelegate extends SliverPersistentHeaderDelegate {
  final TabBar tabBar;
  _TabBarDelegate(this.tabBar);

  @override
  double get minExtent => tabBar.preferredSize.height;
  @override
  double get maxExtent => tabBar.preferredSize.height;

  @override
  Widget build(BuildContext context, double shrinkOffset, bool overlapsContent) {
    return Container(color: Theme.of(context).scaffoldBackgroundColor, child: tabBar);
  }

  @override
  bool shouldRebuild(_TabBarDelegate oldDelegate) => false;
}
```

---

## Part 8 — Followers / Following List Screens

### File: `lib/features/profile/screens/followers_screen.dart`

```dart
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_screenutil/flutter_screenutil.dart';
import 'package:go_router/go_router.dart';
import 'package:cached_network_image/cached_network_image.dart';
import 'package:chatbee/config/theme/app_theme.dart';
import 'package:chatbee/features/profile/repos/follow_repo.dart';
import 'package:chatbee/features/profile/models/user_search_result.dart';

// Used for both followers and following — controlled by [isFollowers] flag
class FollowListScreen extends ConsumerStatefulWidget {
  final String userId;
  final bool isFollowers; // true = followers list, false = following list

  const FollowListScreen({super.key, required this.userId, required this.isFollowers});

  @override
  ConsumerState<FollowListScreen> createState() => _FollowListScreenState();
}

class _FollowListScreenState extends ConsumerState<FollowListScreen> {
  final ScrollController _scrollController = ScrollController();
  List<UserSearchResult> _users = [];
  bool _isLoading = true;
  bool _hasMore = false;
  String? _before;

  @override
  void initState() {
    super.initState();
    _load();
    _scrollController.addListener(_onScroll);
  }

  @override
  void dispose() {
    _scrollController.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final page = widget.isFollowers
          ? await ref.read(followRepoProvider).getFollowers(widget.userId, before: _before)
          : await ref.read(followRepoProvider).getFollowing(widget.userId, before: _before);
      if (mounted) {
        setState(() {
          _users.addAll(page.users);
          _hasMore = page.hasMore;
          _isLoading = false;
          if (page.users.isNotEmpty) _before = page.users.last.id;
        });
      }
    } catch (_) {
      if (mounted) setState(() => _isLoading = false);
    }
  }

  void _onScroll() {
    if (_scrollController.position.pixels >= _scrollController.position.maxScrollExtent - 200 && _hasMore) {
      _load();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text(widget.isFollowers ? 'Followers' : 'Following',
            style: TextStyle(fontSize: 17.sp, fontWeight: FontWeight.w600)),
      ),
      body: _isLoading
          ? const Center(child: CircularProgressIndicator())
          : _users.isEmpty
              ? Center(child: Text('No ${widget.isFollowers ? 'followers' : 'following'} yet',
                  style: TextStyle(fontSize: 15.sp, color: AppTheme.textMediumColor)))
              : ListView.separated(
                  controller: _scrollController,
                  itemCount: _users.length,
                  separatorBuilder: (_, __) => Divider(height: 1, color: AppTheme.borderColor),
                  itemBuilder: (_, i) {
                    final user = _users[i];
                    return ListTile(
                      contentPadding: EdgeInsets.symmetric(horizontal: 16.w, vertical: 6.h),
                      onTap: () => context.push('/profile/${user.id}'),
                      leading: CircleAvatar(
                        radius: 22.r,
                        backgroundColor: AppTheme.borderColor,
                        backgroundImage: user.photoURL.isNotEmpty
                            ? CachedNetworkImageProvider(user.photoURL)
                            : null,
                      ),
                      title: Row(children: [
                        Text(user.displayName,
                            style: TextStyle(fontSize: 15.sp, fontWeight: FontWeight.w600, color: AppTheme.textDarkColor)),
                        if (user.isEditor) ...[
                          SizedBox(width: 4.w),
                          Icon(Icons.verified_rounded, size: 14.r, color: AppTheme.primaryColor),
                        ],
                      ]),
                      subtitle: Text('@${user.username}',
                          style: TextStyle(fontSize: 13.sp, color: AppTheme.textLightColor)),
                    );
                  },
                ),
    );
  }
}
```

---

## Part 9 — Notification Screen UI

### File: `lib/features/notifications/screens/notification_screen.dart`

The notification model and controller already exist from Phase 1. This file just needs the UI screen.

```dart
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_screenutil/flutter_screenutil.dart';
import 'package:go_router/go_router.dart';
import 'package:cached_network_image/cached_network_image.dart';
import 'package:timeago/timeago.dart' as timeago;
import 'package:chatbee/config/theme/app_theme.dart';
import 'package:chatbee/features/notifications/controllers/notification_controller.dart';

class NotificationScreen extends ConsumerWidget {
  const NotificationScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(notificationControllerProvider);

    return Scaffold(
      appBar: AppBar(
        title: Text('Notifications', style: TextStyle(fontSize: 18.sp, fontWeight: FontWeight.w700)),
        centerTitle: false,
        actions: [
          TextButton(
            onPressed: () => ref.read(notificationControllerProvider.notifier).markAllAsRead(),
            child: Text('Mark all read', style: TextStyle(fontSize: 13.sp, color: AppTheme.primaryColor)),
          ),
        ],
      ),
      body: state.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(child: Text(e.toString())),
        data: (notifications) {
          if (notifications.isEmpty) {
            return Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(Icons.notifications_none_rounded, size: 64.r, color: AppTheme.textLightColor),
                  SizedBox(height: 12.h),
                  Text('No notifications yet', style: TextStyle(fontSize: 16.sp, color: AppTheme.textMediumColor)),
                ],
              ),
            );
          }

          return RefreshIndicator(
            onRefresh: () => ref.read(notificationControllerProvider.notifier).refresh(),
            child: ListView.separated(
              itemCount: notifications.length,
              separatorBuilder: (_, __) => Divider(height: 1, color: AppTheme.borderColor),
              itemBuilder: (_, i) {
                final notif = notifications[i];
                return _NotifTile(notif: notif);
              },
            ),
          );
        },
      ),
    );
  }
}

class _NotifTile extends ConsumerWidget {
  final dynamic notif; // use your existing NotificationModel type

  const _NotifTile({required this.notif});

  IconData _iconForType(String type) {
    switch (type) {
      case 'poem_liked':     return Icons.favorite_rounded;
      case 'commented':      return Icons.chat_bubble_rounded;
      case 'comment_liked':  return Icons.favorite_border_rounded;
      case 'reposted':       return Icons.repeat_rounded;
      case 'followed':       return Icons.person_add_rounded;
      case 'mentioned':      return Icons.alternate_email_rounded;
      default:               return Icons.notifications_rounded;
    }
  }

  Color _colorForType(String type) {
    switch (type) {
      case 'poem_liked':
      case 'comment_liked':  return Colors.red;
      case 'followed':       return AppTheme.primaryColor;
      case 'reposted':       return Colors.green;
      case 'mentioned':      return Colors.orange;
      default:               return AppTheme.textMediumColor;
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final isUnread = !(notif.isRead as bool);

    return Container(
      color: isUnread ? AppTheme.primaryColor.withValues(alpha: 0.05) : Colors.transparent,
      child: ListTile(
        contentPadding: EdgeInsets.symmetric(horizontal: 16.w, vertical: 6.h),
        onTap: () {
          ref.read(notificationControllerProvider.notifier).markAsRead(notif.id);
          // Navigate based on resourceType + resourceId
          final type = notif.type as String;
          final resourceType = notif.resourceType as String;
          final resourceId = notif.resourceId as String;

          if (resourceType == 'poem' && resourceId.isNotEmpty) {
            // Navigate to poem — fetch and push
            // For now push to explore feed as fallback
          } else if (type == 'followed') {
            context.push('/profile/${notif.actorId}');
          }
        },
        leading: Stack(
          children: [
            CircleAvatar(
              radius: 22.r,
              backgroundColor: AppTheme.borderColor,
              backgroundImage: (notif.actorPhotoUrl as String?)?.isNotEmpty == true
                  ? CachedNetworkImageProvider(notif.actorPhotoUrl as String)
                  : null,
            ),
            Positioned(
              right: 0, bottom: 0,
              child: Container(
                padding: EdgeInsets.all(3.r),
                decoration: BoxDecoration(
                  color: _colorForType(notif.type as String),
                  shape: BoxShape.circle,
                  border: Border.all(color: AppTheme.surfaceColor, width: 1.5),
                ),
                child: Icon(_iconForType(notif.type as String), size: 10.r, color: Colors.white),
              ),
            ),
          ],
        ),
        title: RichText(
          text: TextSpan(
            children: [
              TextSpan(
                text: notif.actorName as String? ?? 'Someone',
                style: TextStyle(fontSize: 14.sp, fontWeight: FontWeight.w600, color: AppTheme.textDarkColor),
              ),
              TextSpan(
                text: ' ${notif.body}',
                style: TextStyle(fontSize: 14.sp, color: AppTheme.textMediumColor),
              ),
            ],
          ),
        ),
        subtitle: notif.createdAt != null
            ? Text(
                timeago.format(notif.createdAt as DateTime, locale: 'en_short'),
                style: TextStyle(fontSize: 11.sp, color: AppTheme.textLightColor),
              )
            : null,
        trailing: isUnread
            ? Container(
                width: 8.r, height: 8.r,
                decoration: const BoxDecoration(color: AppTheme.primaryColor, shape: BoxShape.circle),
              )
            : null,
      ),
    );
  }
}
```

---

## Part 10 — Router Updates

### File: `lib/core/routes/app_router.dart`

Add these routes:

```dart
GoRoute(
  path: '/profile/:id/followers',
  builder: (context, state) => FollowListScreen(
    userId: state.pathParameters['id']!,
    isFollowers: true,
  ),
),
GoRoute(
  path: '/profile/:id/following',
  builder: (context, state) => FollowListScreen(
    userId: state.pathParameters['id']!,
    isFollowers: false,
  ),
),
GoRoute(
  path: '/notifications',
  builder: (context, state) => const NotificationScreen(),
),
```

Update the Profile tab in the bottom navigation to point to `SelfProfileScreen` instead of whatever it currently points to.

Add a notification bell icon to the app bar of `HomeFeedScreen` or `SelfProfileScreen` that navigates to `/notifications`.

---

## Part 11 — Run Codegen

```bash
dart run build_runner build --delete-conflicting-outputs
```

New files requiring codegen:
- `social_repo.dart`
- Updated `poem_model.dart` (new fields)

---

## Implementation Order for Windsurf

1. Add new endpoints to `ApiEndpoints`
2. Add `isLikedByMe`, `isRepostedByMe`, `isRepost`, `originalPoem` to `PoemModel` — run `build_runner`
3. Create `CommentModel` and `CommentsPage`
4. Create `SocialRepo`
5. Update `PoemCard` to be stateful with like/repost/comment buttons
6. Create `CommentBottomSheet`
7. Create `RepostCard`
8. Create `SelfProfileScreen`
9. Create `FollowListScreen`
10. Create `NotificationScreen`
11. Update router with new routes
12. Run `build_runner`
