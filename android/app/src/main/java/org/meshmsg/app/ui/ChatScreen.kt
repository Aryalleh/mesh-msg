package org.meshmsg.app.ui

import android.Manifest
import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.material.icons.filled.Close
import androidx.compose.runtime.DisposableEffect
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import java.io.File
import org.meshmsg.app.data.MediaFiles
import org.meshmsg.app.data.VoiceRecorder
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TextField
import androidx.compose.material3.TextFieldDefaults
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import org.meshmsg.app.R
import org.meshmsg.app.data.Contact
import org.meshmsg.app.data.Delivery
import org.meshmsg.app.data.GroupKind
import org.meshmsg.app.data.Message
import org.meshmsg.app.ui.theme.MeshTheme
import org.meshmsg.app.ui.theme.Space

/** Text size limits from the core (mobile.TextBytes etc.); a fake in tests. */
data class TextLimits(val onePacket: Int, val max: Int, val bytes: (String) -> Int)

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChatScreen(
    contact: Contact,
    messages: List<Message>,
    anyPathUp: Boolean,
    limits: TextLimits,
    onSend: (String) -> Unit,
    onBack: () -> Unit,
    onRename: (String) -> Unit,
    onRemove: () -> Unit,
    modifier: Modifier = Modifier,
    onInvite: () -> Unit = {},
    onLeave: () -> Unit = {},
    contacts: List<org.meshmsg.app.data.Contact> = emptyList(),
    onAddMembers: (List<String>) -> Unit = {},
    onRemoveMember: (String) -> Unit = {},
    nowMs: Long = System.currentTimeMillis(),
    initialDraft: String = "",
    media: MediaActions = MediaActions(),
) {
    var draft by rememberSaveable(contact.hash) { mutableStateOf(initialDraft) }
    var menu by remember { mutableStateOf(false) }
    var renaming by rememberSaveable { mutableStateOf(false) }
    var removing by rememberSaveable { mutableStateOf(false) }
    var leaving by rememberSaveable { mutableStateOf(false) }
    var showMembers by rememberSaveable { mutableStateOf(false) }
    val group = contact.group
    val list = rememberLazyListState()
    LaunchedEffect(messages.size) { if (messages.isNotEmpty()) list.scrollToItem(messages.size - 1) }

    Scaffold(
        modifier = modifier,
        topBar = {
            TopAppBar(
                navigationIcon = {
                    IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back)) }
                },
                title = {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Space.m)) {
                        Avatar(contact, size = 36.dp)
                        Column {
                            Text(contact.displayName, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            if (group != null) {
                                Text(
                                    when {
                                        group == GroupKind.Private && contact.removed -> stringResource(R.string.group_removed)
                                        group == GroupKind.Private -> stringResource(R.string.group_private_members, contact.members.size)
                                        group == GroupKind.Regional -> stringResource(R.string.group_regional)
                                        else -> stringResource(R.string.group_global)
                                    },
                                    style = MaterialTheme.typography.labelMedium,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                    }
                },
                actions = {
                    IconButton(onClick = { menu = true }) { Icon(Icons.Filled.MoreVert, stringResource(R.string.more_options)) }
                    DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                        if (group != null) {
                            if (group.public) {
                                DropdownMenuItem(text = { Text(stringResource(R.string.group_share)) }, onClick = { menu = false; onInvite() })
                            } else {
                                DropdownMenuItem(text = { Text(stringResource(R.string.group_members)) }, onClick = { menu = false; showMembers = true })
                            }
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.group_leave), color = MaterialTheme.colorScheme.error) },
                                onClick = { menu = false; leaving = true },
                            )
                        } else {
                            DropdownMenuItem(text = { Text(stringResource(R.string.rename)) }, onClick = { menu = false; renaming = true })
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.remove_contact), color = MaterialTheme.colorScheme.error) },
                                onClick = { menu = false; removing = true },
                            )
                        }
                    }
                },
            )
        },
        bottomBar = {
            // Files go to contacts and private groups, not to public chats.
            val files = (group == null || group == GroupKind.Private) && !contact.removed
            Composer(draft, limits, onChange = { draft = it }, onSend = {
                val text = draft.trim()
                if (text.isNotEmpty()) {
                    onSend(text)
                    draft = ""
                }
            }, media = media.takeIf { files }, onMediaSent = { draft = "" })
        },
    ) { pad ->
        Column(Modifier.fillMaxSize().padding(pad)) {
            if (!anyPathUp) InfoBanner(stringResource(R.string.no_path_banner))
            if (group?.public == true && messages.isEmpty()) InfoBanner(stringResource(R.string.group_public_warning))
            if (contact.removed) InfoBanner(stringResource(R.string.group_removed))
            if (messages.isEmpty()) {
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    Text(
                        stringResource(R.string.chat_empty),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        textAlign = TextAlign.Center,
                        modifier = Modifier.padding(Space.xxl),
                    )
                }
            } else {
                LazyColumn(
                    state = list,
                    modifier = Modifier.fillMaxSize(),
                    contentPadding = PaddingValues(horizontal = Space.m, vertical = Space.s),
                    verticalArrangement = Arrangement.spacedBy(Space.xs),
                ) {
                    itemsIndexed(messages, key = { _, m -> m.id }) { i, m ->
                        val newDay = i == 0 || Times.day(messages[i - 1].timeMs) != Times.day(m.timeMs)
                        if (newDay) DayHeader(m.timeMs, nowMs)
                        Bubble(m, showSender = group != null, media = media)
                    }
                }
            }
        }
    }

    if (renaming) {
        var name by rememberSaveable { mutableStateOf(contact.name) }
        AlertDialog(
            onDismissRequest = { renaming = false },
            title = { Text(stringResource(R.string.rename)) },
            text = {
                OutlinedTextField(name, { name = it.take(40) }, label = { Text(stringResource(R.string.contact_name)) }, singleLine = true)
            },
            confirmButton = { TextButton(onClick = { renaming = false; onRename(name) }) { Text(stringResource(R.string.save)) } },
            dismissButton = { TextButton(onClick = { renaming = false }) { Text(stringResource(R.string.cancel)) } },
        )
    }
    if (showMembers) {
        val candidates = contacts.filter { c -> contact.members.none { it.hash == c.hash } }
        var adding by remember { mutableStateOf(false) }
        AlertDialog(
            onDismissRequest = { showMembers = false },
            title = { Text(stringResource(R.string.group_members)) },
            text = {
                Column(Modifier.verticalScroll(rememberScrollState())) {
                    contact.members.forEach { m ->
                        Row(Modifier.fillMaxWidth().heightIn(min = 48.dp), verticalAlignment = Alignment.CenterVertically) {
                            val tags = listOfNotNull(
                                stringResource(R.string.group_owner).takeIf { m.owner },
                                stringResource(R.string.group_you).takeIf { m.you },
                            )
                            Text(
                                m.displayName + if (tags.isEmpty()) "" else " (" + tags.joinToString(", ") + ")",
                                Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis,
                            )
                            if (contact.isOwner && !m.you) {
                                TextButton(
                                    onClick = { onRemoveMember(m.hash) },
                                    colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error),
                                ) { Text(stringResource(R.string.group_remove_member)) }
                            }
                        }
                    }
                    if (contact.isOwner && adding) {
                        candidates.forEach { c ->
                            TextButton(onClick = { onAddMembers(listOf(c.hash)); adding = false }) { Text("+ " + c.displayName) }
                        }
                    }
                }
            },
            confirmButton = { TextButton(onClick = { showMembers = false }) { Text(stringResource(R.string.close)) } },
            dismissButton = {
                if (contact.isOwner && candidates.isNotEmpty() && !adding) {
                    TextButton(onClick = { adding = true }) { Text(stringResource(R.string.group_add_member)) }
                }
            },
        )
    }
    if (leaving) {
        AlertDialog(
            onDismissRequest = { leaving = false },
            title = { Text(stringResource(R.string.group_leave_title, contact.displayName)) },
            text = { Text(stringResource(R.string.group_leave_body)) },
            confirmButton = {
                TextButton(
                    onClick = { leaving = false; onLeave() },
                    colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error),
                ) { Text(stringResource(R.string.group_leave)) }
            },
            dismissButton = { TextButton(onClick = { leaving = false }) { Text(stringResource(R.string.cancel)) } },
        )
    }
    if (removing) {
        AlertDialog(
            onDismissRequest = { removing = false },
            title = { Text(stringResource(R.string.remove_contact_title, contact.displayName)) },
            text = { Text(stringResource(R.string.remove_contact_body)) },
            confirmButton = {
                TextButton(
                    onClick = { removing = false; onRemove() },
                    colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error),
                ) { Text(stringResource(R.string.remove)) }
            },
            dismissButton = { TextButton(onClick = { removing = false }) { Text(stringResource(R.string.cancel)) } },
        )
    }
}

@Composable
private fun DayHeader(ms: Long, nowMs: Long) {
    val today = Times.day(nowMs)
    val label = when (Times.day(ms)) {
        today -> stringResource(R.string.today)
        today.minusDays(1) -> stringResource(R.string.yesterday)
        else -> Times.date(ms, currentLocale())
    }
    Box(Modifier.fillMaxWidth().padding(vertical = Space.s), contentAlignment = Alignment.Center) {
        Text(
            label,
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.semantics { heading() },
        )
    }
}

@Composable
private fun Bubble(m: Message, showSender: Boolean = false, media: MediaActions = MediaActions()) {
    val scheme = MaterialTheme.colorScheme
    val bg = if (m.outgoing) scheme.primaryContainer else scheme.surfaceContainer
    val fg = if (m.outgoing) scheme.onPrimaryContainer else scheme.onSurface
    val meta = fg.copy(alpha = 0.72f)
    // The corner next to the sender's edge is the tail.
    val shape = if (m.outgoing) RoundedCornerShape(20.dp, 20.dp, 4.dp, 20.dp) else RoundedCornerShape(20.dp, 20.dp, 20.dp, 4.dp)
    // A round video message stands alone, without a bubble (once it is here).
    val bare = m.media?.kind == org.meshmsg.app.data.MediaKind.Round && m.media.state == org.meshmsg.app.data.MediaState.Ready && m.text.isEmpty()
    BoxWithConstraints(Modifier.fillMaxWidth()) {
        Column(
            Modifier
                .align(if (m.outgoing) Alignment.CenterEnd else Alignment.CenterStart)
                .widthIn(max = maxWidth * 0.82f)
                .background(if (bare) Color.Transparent else bg, shape)
                .padding(horizontal = Space.m, vertical = Space.s),
        ) {
            if (showSender && !m.outgoing && m.fromName.isNotBlank()) {
                Text(m.fromName, style = MaterialTheme.typography.labelLarge, color = scheme.primary, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            m.media?.let { MediaContent(it, m.outgoing, fg, media) }
            if (m.text.isNotEmpty() || m.media == null) Text(m.text, style = MaterialTheme.typography.bodyLarge, color = fg)
            Row(
                Modifier.align(Alignment.End).padding(top = 2.dp),
                horizontalArrangement = Arrangement.spacedBy(Space.xs),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(Times.clock(m.timeMs, currentLocale()), style = MaterialTheme.typography.labelSmall, color = meta)
                when {
                    !m.outgoing -> {}
                    // Public chats have no delivery receipts: never claim "delivered".
                    m.group && m.recipients == 0 -> DeliveryMark(if (m.delivery == Delivery.Delivered) Delivery.Sent else m.delivery, meta)
                    // Private groups: one receipt per member.
                    m.group && m.deliveredTo < m.recipients -> {
                        val deliveredLabel = stringResource(R.string.delivered_to_n, m.deliveredTo, m.recipients)
                        DeliveryMark(m.delivery, meta)
                        Text(
                            "${m.deliveredTo}/${m.recipients}",
                            style = MaterialTheme.typography.labelSmall,
                            color = meta,
                            modifier = Modifier.semantics { contentDescription = deliveredLabel },
                        )
                    }
                    else -> DeliveryMark(m.delivery, meta)
                }
            }
        }
    }
}

@Composable
private fun Composer(
    draft: String,
    limits: TextLimits,
    onChange: (String) -> Unit,
    onSend: () -> Unit,
    media: MediaActions? = null,
    onMediaSent: () -> Unit = {},
) {
    val ctx = LocalContext.current
    val used = remember(draft) { limits.bytes(draft) }
    var attachMenu by remember { mutableStateOf(false) }
    // The next file also goes through a relay (reaches someone offline; a few per day).
    var important by rememberSaveable { mutableStateOf(false) }
    fun sendFile(f: File, mime: String, caption: String) { media?.sendFile(f, mime, f.name, caption, important); important = false }
    fun pick(uri: android.net.Uri) { media?.pick(uri, draft.trim(), important); important = false; onMediaSent() }
    var recorder by remember { mutableStateOf<VoiceRecorder?>(null) }
    var videoFile by rememberSaveable { mutableStateOf<String?>(null) }
    val micDenied = stringResource(R.string.mic_denied)
    val camDenied = stringResource(R.string.round_permission)
    var roundOpen by rememberSaveable { mutableStateOf(false) }
    var afterCamera by remember { mutableStateOf<(() -> Unit)?>(null) }
    val askCamera = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { res ->
        val go = afterCamera
        afterCamera = null
        if (res[Manifest.permission.CAMERA] == true) go?.invoke() else media?.error(camDenied)
    }
    fun withCamera(perms: Array<String>, go: () -> Unit) {
        if (perms.all { ContextCompat.checkSelfPermission(ctx, it) == PackageManager.PERMISSION_GRANTED }) go()
        else { afterCamera = go; askCamera.launch(perms) }
    }
    if (roundOpen && media != null) {
        RoundVideoRecorder(
            onDone = { f ->
                roundOpen = false
                if (f != null) sendFile(f, RoundVideo.MIME, "")
            },
            onError = { media.error(it) },
        )
    }
    val pickMedia = rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
        if (uri != null && media != null) pick(uri)
    }
    val pickFile = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri != null && media != null) pick(uri)
    }
    val recordVideo = rememberLauncherForActivityResult(RecordVideo()) { ok ->
        val f = videoFile?.let(::File)
        videoFile = null
        if (ok && f != null && f.length() > 0 && media != null) {
            sendFile(f, "video/mp4", draft.trim()); onMediaSent()
        } else f?.delete()
    }
    fun startRecording() {
        recorder = runCatching { VoiceRecorder.start(ctx) }.onFailure { media?.error(it.message ?: it.toString()) }.getOrNull()
    }
    val askMic = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        if (granted) startRecording() else media?.error(micDenied)
    }
    DisposableEffect(Unit) { onDispose { recorder?.cancel() } }
    var elapsed by remember { mutableStateOf(0L) }
    LaunchedEffect(recorder) {
        val r = recorder ?: return@LaunchedEffect
        while (recorder === r) {
            elapsed = System.currentTimeMillis() - r.startedMs
            if (elapsed >= VoiceRecorder.MAX_MS) {
                recorder = null
                r.finish()?.let { sendFile(it, VoiceRecorder.MIME, "") }
            }
            kotlinx.coroutines.delay(250)
        }
    }
    recorder?.let { r ->
        Surface(color = MaterialTheme.colorScheme.surface) {
            Row(
                Modifier.navigationBarsPadding().imePadding().padding(horizontal = Space.m, vertical = Space.s).fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(Space.s),
            ) {
                IconButton(onClick = { recorder = null; r.cancel() }) { Icon(Icons.Filled.Close, stringResource(R.string.voice_cancel)) }
                Text(
                    "● " + stringResource(R.string.voice_recording, "%d:%02d".format(elapsed / 60000, elapsed / 1000 % 60)),
                    Modifier.weight(1f), color = MaterialTheme.colorScheme.error,
                )
                FilledIconButton(onClick = {
                    recorder = null
                    r.finish()?.let { sendFile(it, VoiceRecorder.MIME, "") }
                }, modifier = Modifier.size(48.dp)) { Icon(Icons.AutoMirrored.Filled.Send, stringResource(R.string.voice_send)) }
            }
        }
        return
    }
    val left = limits.onePacket - used
    val tooLong = used > limits.max
    Surface(color = MaterialTheme.colorScheme.surface, tonalElevation = 0.dp) {
        Column(Modifier.navigationBarsPadding().imePadding().padding(horizontal = Space.m, vertical = Space.s)) {
            // Shown only when it matters: near or past one LoRa packet.
            if (left < 24) {
                Text(
                    if (left >= 0) pluralStringResource(R.plurals.bytes_left, left, left) else stringResource(R.string.bytes_over),
                    style = MaterialTheme.typography.labelMedium,
                    color = if (left >= 0) MaterialTheme.colorScheme.onSurfaceVariant else MeshTheme.extra.warning,
                    modifier = Modifier.padding(start = Space.l, bottom = Space.xs),
                )
            }
            if (important && media != null) {
                Text(
                    "⚑ " + stringResource(R.string.important_on),
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.primary,
                    modifier = Modifier.padding(start = Space.l, bottom = Space.xs).clickable { important = false },
                )
            }
            Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                if (media != null) {
                    Box {
                        IconButton(onClick = { attachMenu = true }, modifier = Modifier.size(48.dp)) {
                            Icon(MeshIcons.AttachFile, stringResource(R.string.attach))
                        }
                        DropdownMenu(expanded = attachMenu, onDismissRequest = { attachMenu = false }) {
                            DropdownMenuItem(text = { Text(stringResource(R.string.attach_media)) }, onClick = {
                                attachMenu = false
                                pickMedia.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageAndVideo))
                            })
                            DropdownMenuItem(text = { Text(stringResource(R.string.attach_video)) }, onClick = {
                                attachMenu = false
                                // The app declares CAMERA, so the camera app needs it granted too.
                                withCamera(arrayOf(Manifest.permission.CAMERA)) {
                                    val f = MediaFiles.newOutgoing(ctx, "video", "mp4")
                                    videoFile = f.path
                                    runCatching { recordVideo.launch(MediaFiles.uriFor(ctx, f)) }
                                        .onFailure { videoFile = null; media.error(ctx.getString(R.string.media_no_app)) }
                                }
                            })
                            DropdownMenuItem(text = { Text(stringResource(R.string.attach_file)) }, onClick = {
                                attachMenu = false
                                pickFile.launch(arrayOf("*/*"))
                            })
                            HorizontalDivider()
                            DropdownMenuItem(
                                text = {
                                    Column {
                                        Text(stringResource(R.string.important_toggle))
                                        Text(stringResource(R.string.important_hint), style = MaterialTheme.typography.labelSmall,
                                            color = MaterialTheme.colorScheme.onSurfaceVariant)
                                    }
                                },
                                leadingIcon = { Checkbox(checked = important, onCheckedChange = null) },
                                onClick = { important = !important },
                            )
                        }
                    }
                }
                TextField(
                    value = draft,
                    onValueChange = onChange,
                    modifier = Modifier.weight(1f).heightIn(min = 48.dp),
                    placeholder = { Text(stringResource(R.string.message_hint)) },
                    isError = tooLong,
                    maxLines = 5,
                    shape = RoundedCornerShape(28.dp),
                    keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.Sentences),
                    colors = TextFieldDefaults.colors(
                        focusedContainerColor = MaterialTheme.colorScheme.surfaceContainer,
                        unfocusedContainerColor = MaterialTheme.colorScheme.surfaceContainer,
                        errorContainerColor = MaterialTheme.colorScheme.surfaceContainer,
                        focusedIndicatorColor = Color.Transparent,
                        unfocusedIndicatorColor = Color.Transparent,
                        errorIndicatorColor = Color.Transparent,
                    ),
                )
                if (media != null && draft.isBlank()) {
                    IconButton(
                        onClick = { withCamera(RoundVideo.permissions) { roundOpen = true } },
                        modifier = Modifier.size(48.dp),
                    ) { Icon(MeshIcons.RoundVideo, stringResource(R.string.round_record)) }
                    FilledIconButton(onClick = {
                        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) startRecording()
                        else askMic.launch(Manifest.permission.RECORD_AUDIO)
                    }, modifier = Modifier.size(48.dp)) { Icon(MeshIcons.Mic, stringResource(R.string.voice_record)) }
                } else {
                    FilledIconButton(
                        onClick = onSend,
                        enabled = draft.isNotBlank() && !tooLong,
                        modifier = Modifier.size(48.dp),
                    ) { Icon(Icons.AutoMirrored.Filled.Send, stringResource(R.string.send)) }
                }
            }
        }
    }
}
