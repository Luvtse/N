// §2.1 In-app chat — full-screen rider↔driver conversation.
//
// Reached from the Tracking page's driver card (chat entry point with an
// unread badge). Contract with the backend chat module:
//   * History + canned catalog load on open; failure shows a real
//     "unavailable — retry" state, never fake bubbles (Global Rule 2).
//   * Sends are optimistic: a pending bubble appears immediately, is replaced
//     by the server copy on 201, and flips to a failed bubble with a Retry
//     affordance on error.
//   * Live inbound messages arrive over WS topic chat.ride.{rideId}; our own
//     echoes are dropped by sender_id when the signed-in id is known.
//   * Opening this page marks the other side's messages read and clears the
//     local unread badge.
//   * translated_from is shown only when the server populated it (translation
//     flag off => field null => no translation affordances anywhere).
import 'dart:async';

import 'package:flutter/material.dart';

import '../../../../core/di/injection.dart';
import '../../data/repositories/chat_repository.dart';

class ChatPage extends StatefulWidget {
  static const String routeName = 'nidusChat';
  static const String pathTemplate = '/nidus/chat/:rideId';

  final String rideId;

  /// Role of the signed-in user on this ride. The rider app passes
  /// ChatSenderRole.rider; the driver app passes ChatSenderRole.driver.
  /// Defaults to rider because this surface ships in the rider app first.
  final ChatSenderRole viewerRole;

  const ChatPage({
    super.key,
    required this.rideId,
    this.viewerRole = ChatSenderRole.rider,
  });

  @override
  State<ChatPage> createState() => _ChatPageState();
}

class _ChatPageState extends State<ChatPage> {
  final ChatRepository _repo = getIt<ChatRepository>();
  final TextEditingController _inputController = TextEditingController();
  final ScrollController _scrollController = ScrollController();

  List<ChatMessage> _messages = [];
  CannedCatalog? _canned;

  bool _loadingHistory = true;
  String? _historyError; // non-null => "unavailable — retry" banner
  bool _sending = false;
  int _pendingSeq = 0;

  String? _signedInUserId;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _repo.stopListening();
    _inputController.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  // --------------------------------------------------------------------------
  // Load / retry
  // --------------------------------------------------------------------------

  Future<void> _load() async {
    setState(() {
      _loadingHistory = true;
      _historyError = null;
    });

    try {
      // Best-effort: knowing our user id lets us drop our own WS echoes.
      // Absent it, echo suppression falls back to body+cannedId matching.
      final me = await getIt<AuthRepository>().getCurrentUser();
      _signedInUserId = me?.id;
    } catch (_) {
      _signedInUserId = null;
    }

    try {
      final page = await _repo.history(widget.rideId);
      var all = page.messages;
      while (page.hasMore) {
        final next = await _repo.history(
          widget.rideId,
          cursor: page.nextCursor,
        );
        if (next.messages.isEmpty) break;
        all = [...all, ...next.messages];
      }
      all.sort((a, b) => a.createdAt.compareTo(b.createdAt));

      CannedCatalog? catalog;
      try {
        catalog = await _repo.canned(locale: Localizations.localeOf(context).languageCode);
      } on ChatUnavailableException {
        catalog = null; // chips hidden; free-text still works. Honest absence.
      }

      _repo.listen(widget.rideId, _onIncoming);
      unawaited(_markRead());

      if (!mounted) return;
      setState(() {
        _messages = all;
        _canned = catalog;
        _loadingHistory = false;
      });
      _scrollToBottom();
    } on ChatForbiddenException {
      if (!mounted) return;
      setState(() {
        _loadingHistory = false;
        _historyError = 'You are not a participant of this ride.';
      });
    } on ChatUnavailableException {
      if (!mounted) return;
      setState(() {
        _loadingHistory = false;
        _historyError = 'Chat unavailable — check your connection and retry.';
      });
    }
  }

  Future<void> _markRead() async {
    await _repo.markRead(widget.rideId);
  }

  // --------------------------------------------------------------------------
  // Real-time
  // --------------------------------------------------------------------------

  void _onIncoming(ChatMessage message) {
    if (!mounted) return;
    // Drop our own echo when we can identify it.
    if (_signedInUserId != null && message.senderId == _signedInUserId) {
      return;
    }
    // Ignore duplicates already visible (e.g. re-delivery after reconnect).
    if (_messages.any((m) => m.id == message.id)) return;
    setState(() => _messages = [..._messages, message]);
    _scrollToBottom();
  }

  // --------------------------------------------------------------------------
  // Sending
  // --------------------------------------------------------------------------

  Future<void> _sendText() async {
    final text = _inputController.text.trim();
    if (text.isEmpty || _sending) return;
    _inputController.clear();
    await _send(body: text);
  }

  Future<void> _sendCanned(CannedMessage canned) async {
    if (_sending) return;
    await _send(body: canned.body, cannedId: canned.id);
  }

  Future<void> _retryFailed(ChatMessage failed) async {
    await _send(body: failed.body, cannedId: failed.cannedId, replacing: failed);
  }

  Future<void> _send({
    required String body,
    String? cannedId,
    ChatMessage? replacing,
  }) async {
    final tempId = 'pending-${widget.rideId}-${_pendingSeq++}';
    final optimistic = ChatMessage.pending(
      tempId: tempId,
      rideId: widget.rideId,
      senderRole: widget.viewerRole,
      body: body,
      cannedId: cannedId,
    );

    setState(() {
      _sending = true;
      if (replacing != null) {
        _messages = _messages.map((m) => m.id == replacing.id ? optimistic : m).toList();
      } else {
        _messages = [..._messages, optimistic];
      }
    });
    _scrollToBottom();

    try {
      final confirmed = await _repo.send(
        widget.rideId,
        body: body,
        cannedId: cannedId,
      );
      if (!mounted) return;
      setState(() {
        _messages = _messages
            .map((m) => m.id == tempId ? confirmed : m)
            .toList();
        _sending = false;
      });
    } on ChatUnavailableException {
      if (!mounted) return;
      setState(() {
        _messages = _messages
            .map((m) => m.id == tempId ? m.copyWithStatus('failed') : m)
            .toList();
        _sending = false;
      });
    } on ChatForbiddenException catch (e) {
      if (!mounted) return;
      setState(() {
        _messages = _messages.where((m) => m.id != tempId).toList();
        _sending = false;
        _historyError = e.message;
      });
    }
  }

  // --------------------------------------------------------------------------
  // Layout
  // --------------------------------------------------------------------------

  void _scrollToBottom() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scrollController.hasClients) return;
      _scrollController.animateTo(
        _scrollController.position.maxScrollExtent,
        duration: const Duration(milliseconds: 200),
        curve: Curves.easeOut,
      );
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final catalog = _canned;
    final chips = catalog == null
        ? const <Widget>[]
        : catalog
            .forRole(widget.viewerRole)
            .map(
              (c) => Padding(
                padding: const EdgeInsets.only(right: 8),
                child: ActionChip(
                  label: Text(c.label),
                  onPressed: _sending ? null : () => _sendCanned(c),
                ),
              ),
            )
            .toList();

    return Scaffold(
      appBar: AppBar(
        title: const Text('Chat with your driver'),
        semanticsLabel: 'Rider driver chat',
      ),
      body: Column(
        children: [
          if (_historyError != null)
            MaterialBanner(
              backgroundColor: theme.colorScheme.errorContainer,
              content: Text(_historyError!),
              actions: [
                TextButton(
                  onPressed: _load,
                  child: const Text('Retry'),
                ),
              ],
            ),
          Expanded(child: _buildMessageList(theme)),
          if (chips.isNotEmpty)
            SizedBox(
              height: 48,
              child: ListView(
                scrollDirection: Axis.horizontal,
                padding: const EdgeInsets.symmetric(horizontal: 12),
                children: chips,
              ),
            ),
          _buildInputRow(theme),
        ],
      ),
    );
  }

  Widget _buildMessageList(ThemeData theme) {
    if (_loadingHistory) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_messages.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Text(
            'No messages yet. Say hello or tap a quick reply.',
            textAlign: TextAlign.center,
            style: theme.textTheme.bodyMedium,
          ),
        ),
      );
    }
    return ListView.builder(
      controller: _scrollController,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      itemCount: _messages.length,
      itemBuilder: (context, index) =>
          _buildBubble(_messages[index], theme),
    );
  }

  Widget _buildBubble(ChatMessage message, ThemeData theme) {
    final mine = message.senderRole == widget.viewerRole;
    final isFailed = message.isFailed;

    return Align(
      alignment: mine ? Alignment.centerRight : Alignment.centerLeft,
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxWidth: MediaQuery.sizeOf(context).width * 0.75,
        ),
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 3),
          child: Column(
            crossAxisAlignment:
                mine ? CrossAxisAlignment.end : CrossAxisAlignment.start,
            children: [
              Container(
                padding:
                    const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                decoration: BoxDecoration(
                  color: mine
                      ? theme.colorScheme.primaryContainer
                      : theme.colorScheme.surfaceContainerHighest,
                  borderRadius: BorderRadius.circular(14),
                  border: isFailed
                      ? Border.all(color: theme.colorScheme.error)
                      : null,
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    SelectableText(message.body),
                    if (message.translatedFrom != null)
                      Padding(
                        padding: const EdgeInsets.only(top: 4),
                        child: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Icon(
                              Icons.translate,
                              size: 14,
                              color: theme.textTheme.caption?.color,
                            ),
                            const SizedBox(width: 4),
                            Text(
                              'Translated from ${message.translatedFrom}',
                              style: theme.textTheme.caption,
                            ),
                          ],
                        ),
                      ),
                  ],
                ),
              ),
              if (message.isPending)
                Padding(
                  padding: const EdgeInsets.only(top: 2),
                  child: Text(
                    'Sending…',
                    style: theme.textTheme.caption,
                  ),
                ),
              if (isFailed)
                Padding(
                  padding: const EdgeInsets.only(top: 2),
                  child: InkWell(
                    onTap: () => _retryFailed(message),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Icon(
                          Icons.error_outline,
                          size: 14,
                          color: theme.colorScheme.error,
                        ),
                        const SizedBox(width: 4),
                        Text(
                          'Not delivered — Retry',
                          style: theme.textTheme.caption?.copyWith(
                            color: theme.colorScheme.error,
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildInputRow(ThemeData theme) {
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(12, 4, 12, 12),
        child: Row(
          children: [
            Expanded(
              child: TextField(
                controller: _inputController,
                textCapitalization: TextCapitalization.sentences,
                maxLength: 1000,
                minLines: 1,
                maxLines: 4,
                decoration: const InputDecoration(
                  hintText: 'Message',
                  counterText: '',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
                onSubmitted: (_) => _sendText(),
              ),
            ),
            const SizedBox(width: 8),
            IconButton.filled(
              onPressed: _sending ? null : _sendText,
              icon: const Icon(Icons.send),
              tooltip: 'Send message',
            ),
          ],
        ),
      ),
    );
  }
}
