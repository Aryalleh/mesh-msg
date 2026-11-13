package org.meshmsg.app.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material3.Badge
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExtendedFloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import org.meshmsg.app.R
import org.meshmsg.app.data.Chat
import org.meshmsg.app.ui.theme.Space

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChatsScreen(
    chats: List<Chat>,
    anyPathUp: Boolean,
    onOpen: (Chat) -> Unit,
    onAdd: () -> Unit,
    modifier: Modifier = Modifier,
    nowMs: Long = System.currentTimeMillis(),
    update: UpdateBannerState? = null,
) {
    Scaffold(
        modifier = modifier,
        topBar = { TopAppBar(title = { Text(stringResource(R.string.nav_chats)) }) },
        floatingActionButton = {
            if (chats.isNotEmpty()) {
                ExtendedFloatingActionButton(
                    onClick = onAdd,
                    icon = { Icon(Icons.Filled.Add, contentDescription = null) },
                    text = { Text(stringResource(R.string.new_chat)) },
                )
            }
        },
    ) { pad ->
        Column(Modifier.fillMaxSize().padding(pad)) {
            update?.let { UpdateBanner(it) }
            if (!anyPathUp && chats.isNotEmpty()) InfoBanner(stringResource(R.string.no_path_banner))
            if (chats.isEmpty()) {
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    EmptyState(stringResource(R.string.chats_empty_title), stringResource(R.string.chats_empty_body)) {
                        Button(onClick = onAdd, modifier = Modifier.heightIn(min = 48.dp)) {
                            Icon(Icons.Filled.Add, contentDescription = null)
                            Text(stringResource(R.string.new_chat), Modifier.padding(start = Space.s))
                        }
                    }
                }
            } else {
                // Bottom padding keeps the last row clear of the FAB.
                LazyColumn(contentPadding = PaddingValues(bottom = 88.dp)) {
                    items(chats, key = { it.contact.hash }) { chat -> ChatRow(chat, nowMs) { onOpen(chat) } }
                }
            }
        }
    }
}

@Composable
private fun ChatRow(chat: Chat, nowMs: Long, onClick: () -> Unit) {
    val locale = currentLocale()
    val last = chat.last
    val unread = chat.unread > 0
    Row(
        Modifier.fillMaxWidth().heightIn(min = 72.dp).clickable(onClick = onClick).padding(horizontal = Space.l, vertical = Space.m),
        horizontalArrangement = Arrangement.spacedBy(Space.m),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Avatar(chat.contact)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(
                chat.contact.displayName,
                style = MaterialTheme.typography.titleMedium,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(Space.xs), verticalAlignment = Alignment.CenterVertically) {
                if (last != null && last.outgoing) DeliveryMark(last.delivery, MaterialTheme.colorScheme.onSurfaceVariant)
                val sender = if (last != null && chat.contact.isGroup && !last.outgoing && last.fromName.isNotBlank()) last.fromName + ": " else ""
                Text(
                    last?.let { l ->
                        val media = l.media?.let { "📎 " + (if (l.text.isBlank()) mediaLabel(it) else "") } ?: ""
                        sender + media + l.text.replace('\n', ' ')
                    } ?: stringResource(R.string.chat_no_messages),
                    style = MaterialTheme.typography.bodyMedium,
                    color = if (unread) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
                    fontWeight = if (unread) FontWeight.Medium else FontWeight.Normal,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        Column(horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(Space.xs)) {
            if (last != null) {
                Text(
                    Times.short(last.timeMs, nowMs, locale),
                    style = MaterialTheme.typography.labelSmall,
                    color = if (unread) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (unread) {
                val label = pluralStringResource(R.plurals.unread_count, chat.unread, chat.unread)
                Badge(
                    containerColor = MaterialTheme.colorScheme.primary,
                    contentColor = MaterialTheme.colorScheme.onPrimary,
                    modifier = Modifier.semantics { contentDescription = label },
                ) { Text(if (chat.unread > 99) "99+" else "%d".format(locale, chat.unread)) }
            }
        }
    }
}
