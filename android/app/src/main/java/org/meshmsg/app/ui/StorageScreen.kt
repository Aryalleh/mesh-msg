package org.meshmsg.app.ui

import androidx.compose.animation.AnimatedContent
import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DateRangePicker
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.rememberDateRangePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import java.text.DateFormat
import java.util.Date
import org.meshmsg.app.R
import org.meshmsg.app.data.Contact
import org.meshmsg.app.data.StoragePick
import org.meshmsg.app.data.StoredFile
import org.meshmsg.app.ui.theme.Space

/** Time ranges offered as chips; [Custom] opens a date range picker. */
private enum class Span(val days: Int) { All(0), Week(7), Month(30), Quarter(90), Custom(-1) }

private val Kinds = listOf(
    "image" to R.string.media_photo,
    "video" to R.string.media_video,
    "round" to R.string.media_round,
    "audio" to R.string.media_voice,
    "file" to R.string.media_file,
)

/**
 * Files kept on this phone, per chat: pick chats, kinds and a time range (any
 * mix) and delete them. Messages stay in the chats ("deleted").
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun StorageScreen(
    files: List<StoredFile>?,
    contacts: List<Contact>,
    onDelete: (Collection<String>) -> Unit,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var span by rememberSaveable { mutableStateOf(Span.All) }
    var customFrom by rememberSaveable { mutableStateOf(0L) }
    var customTo by rememberSaveable { mutableStateOf(0L) }
    var kinds by rememberSaveable { mutableStateOf(setOf<String>()) }
    // null: every chat
    var chats by rememberSaveable { mutableStateOf<Set<String>?>(null) }
    var picking by rememberSaveable { mutableStateOf(false) }
    var confirming by rememberSaveable { mutableStateOf(false) }

    val now = remember(files) { System.currentTimeMillis() }
    val pick = when (span) {
        Span.All -> StoragePick(chats, kinds)
        Span.Custom -> StoragePick(chats, kinds, customFrom, customTo)
        // "Older than": everything up to that many days ago.
        else -> StoragePick(chats, kinds, toMs = now - span.days * 86_400_000L)
    }
    val all = files.orEmpty()
    // Per-chat sizes follow the kind and time choice (not the chat choice).
    val byChat = remember(all, pick.kinds, pick.fromMs, pick.toMs) {
        all.filter { pick.copy(chats = null).matches(it) }.groupBy { it.chat }
            .map { (c, fs) -> Triple(c, fs.size, fs.sumOf { it.bytes }) }
            .sortedByDescending { it.third }
    }
    val chosen = remember(all, pick) { all.filter(pick::matches) }
    val chosenBytes = chosen.sumOf { it.bytes }
    val names = remember(contacts) { contacts.associate { it.hash to it.displayName } }
    val other = stringResource(R.string.storage_other)

    Scaffold(
        modifier = modifier,
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.storage_title)) },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back)) } },
            )
        },
        bottomBar = {
            Surface(tonalElevation = 3.dp) {
                Button(
                    onClick = { confirming = true },
                    enabled = chosen.isNotEmpty(),
                    colors = ButtonDefaults.buttonColors(containerColor = MaterialTheme.colorScheme.error, contentColor = MaterialTheme.colorScheme.onError),
                    modifier = Modifier.fillMaxWidth().navigationBarsPadding().padding(Space.l).heightIn(min = 52.dp),
                ) {
                    Icon(Icons.Filled.Delete, null, Modifier.size(20.dp))
                    Spacer(Modifier.size(Space.s))
                    AnimatedContent(chosen.size to chosenBytes, label = "deleteCount") { (n, b) ->
                        Text(if (n == 0) stringResource(R.string.storage_nothing) else stringResource(R.string.storage_delete_n, n, humanSize(b)))
                    }
                }
            }
        },
    ) { pad ->
        if (files == null) {
            LinearProgressIndicator(Modifier.fillMaxWidth().padding(pad))
            return@Scaffold
        }
        LazyColumn(Modifier.fillMaxSize().padding(pad)) {
            item {
                Column(Modifier.padding(horizontal = Space.l, vertical = Space.m)) {
                    Text(humanSize(all.sumOf { it.bytes }), style = MaterialTheme.typography.headlineMedium)
                    Text(
                        stringResource(R.string.storage_total, all.size),
                        style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                    Text(
                        stringResource(R.string.storage_help),
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(top = Space.s),
                    )
                }
            }
            item { SectionHeader(stringResource(R.string.storage_when)) }
            item {
                val df = remember { DateFormat.getDateInstance(DateFormat.SHORT) }
                Row(Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = Space.l), horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                    Span.entries.forEach { s ->
                        val label = when (s) {
                            Span.All -> stringResource(R.string.storage_all_time)
                            Span.Week -> stringResource(R.string.storage_older_week)
                            Span.Month -> stringResource(R.string.storage_older_month)
                            Span.Quarter -> stringResource(R.string.storage_older_3months)
                            Span.Custom -> if (span == Span.Custom && customFrom > 0) "${df.format(Date(customFrom))} – ${df.format(Date(customTo))}" else stringResource(R.string.storage_custom)
                        }
                        FilterChip(selected = span == s, onClick = { if (s == Span.Custom) picking = true else span = s }, label = { Text(label) })
                    }
                }
            }
            item { SectionHeader(stringResource(R.string.storage_kinds)) }
            item {
                Row(Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = Space.l), horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                    FilterChip(selected = kinds.isEmpty(), onClick = { kinds = emptySet() }, label = { Text(stringResource(R.string.storage_all_kinds)) })
                    Kinds.forEach { (k, label) ->
                        FilterChip(
                            selected = k in kinds,
                            onClick = { kinds = if (k in kinds) kinds - k else kinds + k },
                            label = { Text(stringResource(label)) },
                        )
                    }
                }
            }
            item {
                Row(Modifier.fillMaxWidth().padding(end = Space.s), verticalAlignment = Alignment.CenterVertically) {
                    SectionHeader(stringResource(R.string.storage_chats), Modifier.weight(1f))
                    TextButton(onClick = { chats = if (chats == null) emptySet() else null }) {
                        Text(stringResource(if (chats == null) R.string.storage_select_none else R.string.storage_select_all))
                    }
                }
            }
            if (byChat.isEmpty()) {
                item {
                    Text(
                        stringResource(R.string.storage_empty),
                        style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(Space.l),
                    )
                }
            }
            items(byChat, key = { it.first }) { (chat, n, bytes) ->
                val on = chats?.contains(chat) ?: true
                val share = if (all.isEmpty()) 0f else (bytes.toFloat() / maxOf(1L, byChat.first().third)).coerceIn(0f, 1f)
                Row(
                    Modifier.fillMaxWidth().heightIn(min = 64.dp)
                        .toggleable(value = on, role = Role.Checkbox) { v ->
                            val cur = chats ?: byChat.map { it.first }.toSet()
                            val next = if (v) cur + chat else cur - chat
                            chats = if (next.containsAll(byChat.map { it.first })) null else next
                        }
                        .padding(horizontal = Space.l, vertical = Space.s)
                        .animateContentSize(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(Space.m),
                ) {
                    Checkbox(checked = on, onCheckedChange = null)
                    Column(Modifier.weight(1f)) {
                        Text(names[chat] ?: other, style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        LinearProgressIndicator(progress = { share }, Modifier.fillMaxWidth().padding(vertical = 4.dp))
                        Text(
                            stringResource(R.string.storage_chat_line, n, humanSize(bytes)),
                            style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                HorizontalDivider(Modifier.padding(start = 72.dp))
            }
        }
    }

    if (picking) {
        val st = rememberDateRangePickerState(
            initialSelectedStartDateMillis = customFrom.takeIf { it > 0 },
            initialSelectedEndDateMillis = customTo.takeIf { it > 0 }?.minus(86_399_999L),
        )
        DatePickerDialog(
            onDismissRequest = { picking = false },
            confirmButton = {
                TextButton(
                    enabled = st.selectedStartDateMillis != null,
                    onClick = {
                        val from = st.selectedStartDateMillis ?: 0L
                        // The end day counts whole (to its last millisecond).
                        val to = (st.selectedEndDateMillis ?: from) + 86_399_999L
                        customFrom = from; customTo = to; span = Span.Custom; picking = false
                    },
                ) { Text(stringResource(R.string.ok)) }
            },
            dismissButton = { TextButton(onClick = { picking = false }) { Text(stringResource(R.string.cancel)) } },
        ) {
            DateRangePicker(state = st, modifier = Modifier.weight(1f), title = { Text(stringResource(R.string.storage_custom), Modifier.padding(Space.l)) })
        }
    }

    if (confirming) {
        AlertDialog(
            onDismissRequest = { confirming = false },
            title = { Text(stringResource(R.string.storage_confirm_title, chosen.size)) },
            text = { Text(stringResource(R.string.storage_confirm_body, humanSize(chosenBytes))) },
            confirmButton = {
                TextButton(onClick = { onDelete(chosen.map { it.id }); confirming = false }) {
                    Text(stringResource(R.string.delete), color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = { TextButton(onClick = { confirming = false }) { Text(stringResource(R.string.cancel)) } },
        )
    }
}
