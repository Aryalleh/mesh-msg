package org.meshmsg.app.ui

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Checkbox
import androidx.compose.material3.FilterChip
import androidx.compose.material3.IconButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.TextButton
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.toMutableStateList
import androidx.compose.ui.semantics.Role
import org.meshmsg.app.data.ChatFolder
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
    folders: List<ChatFolder> = emptyList(),
    onSaveFolders: (List<ChatFolder>) -> Unit = {},
) {
    var folder by rememberSaveable { mutableStateOf(FOLDER_ALL) }
    var editing by rememberSaveable { mutableStateOf(false) }
    var filing by remember { mutableStateOf<Chat?>(null) }
    if (folder.startsWith("f:") && folders.none { "f:" + it.name == folder }) folder = FOLDER_ALL
    val shown = chats.filter { inFolder(it, folder, folders) }
    if (editing) FolderEditor(folders, onSaveFolders) { editing = false }
    filing?.let { c -> FileChat(c, folders, onSaveFolders, onEdit = { editing = true }) { filing = null } }
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
                FolderTabs(chats, folders, folder, onSelect = { folder = it }, onEdit = { editing = true })
                if (shown.isEmpty()) {
                    Text(
                        stringResource(R.string.folder_empty),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(Space.l),
                    )
                }
                // Bottom padding keeps the last row clear of the FAB.
                LazyColumn(contentPadding = PaddingValues(bottom = 88.dp)) {
                    items(shown, key = { it.contact.hash }) { chat -> ChatRow(chat, nowMs, onLongClick = { filing = chat }) { onOpen(chat) } }
                }
            }
        }
    }
}

private const val FOLDER_ALL = "all"
private const val FOLDER_PERSONAL = "personal"
private const val FOLDER_GROUPS = "groups"
private const val FOLDER_UNREAD = "unread"

private fun inFolder(c: Chat, key: String, folders: List<ChatFolder>): Boolean = when (key) {
    FOLDER_PERSONAL -> !c.contact.isGroup
    FOLDER_GROUPS -> c.contact.isGroup
    FOLDER_UNREAD -> c.unread > 0
    FOLDER_ALL -> true
    else -> folders.firstOrNull { "f:" + it.name == key }?.chats?.contains(c.contact.hash) ?: true
}

/** Telegram-style folder tabs: built-in views, then the user's own. */
@Composable
private fun FolderTabs(chats: List<Chat>, folders: List<ChatFolder>, selected: String, onSelect: (String) -> Unit, onEdit: () -> Unit) {
    val tabs = listOf(
        FOLDER_ALL to stringResource(R.string.folder_all),
        FOLDER_PERSONAL to stringResource(R.string.folder_personal),
        FOLDER_GROUPS to stringResource(R.string.folder_groups),
        FOLDER_UNREAD to stringResource(R.string.folder_unread),
    ) + folders.map { "f:" + it.name to it.name }
    LazyRow(
        contentPadding = PaddingValues(horizontal = Space.l, vertical = Space.xs),
        horizontalArrangement = Arrangement.spacedBy(Space.s),
    ) {
        items(tabs, key = { it.first }) { (key, name) ->
            val unread = if (key == FOLDER_UNREAD) 0 else chats.count { it.unread > 0 && inFolder(it, key, folders) }
            FilterChip(
                selected = key == selected,
                onClick = { onSelect(key) },
                label = { Text(if (unread > 0) "$name · $unread" else name, maxLines = 1) },
            )
        }
        item {
            AssistChip(
                onClick = onEdit,
                label = { Text(stringResource(R.string.folders_edit)) },
                leadingIcon = { Icon(Icons.Filled.Edit, contentDescription = null, Modifier.size(18.dp)) },
            )
        }
    }
}

/** Renames, adds and deletes folders. */
@Composable
private fun FolderEditor(folders: List<ChatFolder>, onSave: (List<ChatFolder>) -> Unit, onDismiss: () -> Unit) {
    val draft = remember { folders.toMutableStateList() }
    val newName = stringResource(R.string.folders_new_name)
    val names = draft.map { it.name.trim() }
    val valid = names.all { it.isNotEmpty() && it.length <= 24 } && names.map { it.lowercase() }.toSet().size == names.size
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.folders_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(Space.s)) {
                Text(stringResource(R.string.folders_hint), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                draft.forEachIndexed { i, f ->
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        OutlinedTextField(
                            value = f.name,
                            onValueChange = { draft[i] = f.copy(name = it.take(24)) },
                            singleLine = true,
                            modifier = Modifier.weight(1f),
                        )
                        IconButton(onClick = { draft.removeAt(i) }) {
                            Icon(Icons.Filled.Delete, contentDescription = stringResource(R.string.remove))
                        }
                    }
                }
                TextButton(onClick = { draft.add(ChatFolder("$newName ${draft.size + 1}", emptyList())) }) {
                    Icon(Icons.Filled.Add, contentDescription = null)
                    Text(stringResource(R.string.folders_new), Modifier.padding(start = Space.s))
                }
            }
        },
        confirmButton = {
            TextButton(enabled = valid, onClick = { onSave(draft.map { it.copy(name = it.name.trim()) }); onDismiss() }) { Text(stringResource(R.string.save)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel)) } },
    )
}

/** Long-press on a chat: which folders it is in. */
@Composable
private fun FileChat(chat: Chat, folders: List<ChatFolder>, onSave: (List<ChatFolder>) -> Unit, onEdit: () -> Unit, onDismiss: () -> Unit) {
    val h = chat.contact.hash
    val inSet = remember { folders.filter { h in it.chats }.map { it.name }.toMutableStateList() }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.folder_add_title, chat.contact.displayName)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                if (folders.isEmpty()) Text(stringResource(R.string.folders_none), style = MaterialTheme.typography.bodyMedium)
                folders.forEach { f ->
                    Row(
                        Modifier.fillMaxWidth().heightIn(min = 48.dp).toggleable(f.name in inSet, role = Role.Checkbox) {
                            if (it) inSet.add(f.name) else inSet.remove(f.name)
                        },
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Checkbox(checked = f.name in inSet, onCheckedChange = null)
                        Text(f.name, Modifier.padding(start = Space.m))
                    }
                }
                TextButton(onClick = { onDismiss(); onEdit() }) { Text(stringResource(R.string.folders_edit)) }
            }
        },
        confirmButton = {
            TextButton(onClick = {
                onSave(folders.map { f -> f.copy(chats = if (f.name in inSet) (f.chats - h) + h else f.chats - h) })
                onDismiss()
            }) { Text(stringResource(R.string.save)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel)) } },
    )
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun ChatRow(chat: Chat, nowMs: Long, onLongClick: () -> Unit = {}, onClick: () -> Unit) {
    val locale = currentLocale()
    val last = chat.last
    val unread = chat.unread > 0
    Row(
        Modifier.fillMaxWidth().heightIn(min = 72.dp).combinedClickable(onLongClick = onLongClick, onClick = onClick).padding(horizontal = Space.l, vertical = Space.m),
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
