package org.meshmsg.app.ui

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.scale
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import org.meshmsg.app.R
import org.meshmsg.app.ui.theme.Space
import org.meshmsg.app.data.Contact

private enum class MergeStep { Pick, Confirm, Running, Done }

/**
 * Merges [from] (an account the contact lost) into another contact: pick the
 * new account, confirm, then watch the old chat fly into the new one.
 */
@Composable
fun MergeDialog(
    from: Contact,
    candidates: List<Contact>,
    merge: suspend (String) -> Int?,
    onDismiss: () -> Unit,
    onDone: (String) -> Unit,
) {
    var step by rememberSaveable { mutableStateOf(MergeStep.Pick) }
    var target by rememberSaveable { mutableStateOf<String?>(null) }
    var moved by rememberSaveable { mutableStateOf(0) }
    val into = candidates.firstOrNull { it.hash == target }

    when (step) {
        MergeStep.Pick -> AlertDialog(
            onDismissRequest = onDismiss,
            title = { Text(stringResource(R.string.merge_pick_title, from.displayName)) },
            text = {
                Column(Modifier.verticalScroll(rememberScrollState())) {
                    Text(
                        stringResource(R.string.merge_pick_body),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(bottom = Space.s),
                    )
                    candidates.forEach { c ->
                        Row(
                            Modifier.fillMaxWidth().heightIn(min = 56.dp)
                                .selectable(selected = c.hash == target, role = Role.RadioButton, onClick = { target = c.hash }),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(Space.m),
                        ) {
                            RadioButton(selected = c.hash == target, onClick = null)
                            Avatar(c, size = 36.dp)
                            Text(c.displayName, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                    }
                }
            },
            confirmButton = {
                TextButton(enabled = into != null, onClick = { step = MergeStep.Confirm }) { Text(stringResource(R.string.next)) }
            },
            dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel)) } },
        )
        MergeStep.Confirm -> AlertDialog(
            onDismissRequest = onDismiss,
            title = { Text(stringResource(R.string.merge_confirm_title)) },
            text = { Text(stringResource(R.string.merge_confirm_body, from.displayName, into?.displayName ?: "")) },
            confirmButton = { TextButton(onClick = { step = MergeStep.Running }) { Text(stringResource(R.string.merge_do)) } },
            dismissButton = { TextButton(onClick = { step = MergeStep.Pick }) { Text(stringResource(R.string.back)) } },
        )
        MergeStep.Running, MergeStep.Done -> {
            val dest = into ?: return
            val fly = remember { Animatable(0f) }
            if (step == MergeStep.Running) {
                LaunchedEffect(Unit) {
                    val n = coroutineScope {
                        val job = async { merge(dest.hash) }
                        delay(900) // let the "moving" part be seen even when the merge is instant
                        job.await()
                    }
                    if (n == null) {
                        onDismiss()
                        return@LaunchedEffect
                    }
                    moved = n
                    fly.animateTo(1f, tween(650, easing = FastOutSlowInEasing))
                    step = MergeStep.Done
                }
            }
            AlertDialog(
                onDismissRequest = { if (step == MergeStep.Done) onDone(dest.hash) },
                title = {
                    Text(
                        stringResource(if (step == MergeStep.Done) R.string.merge_done_title else R.string.merge_running),
                        textAlign = TextAlign.Center, modifier = Modifier.fillMaxWidth(),
                    )
                },
                text = {
                    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally) {
                        MergeAnimation(from, dest, if (step == MergeStep.Done) 1f else fly.value, running = step == MergeStep.Running)
                        if (step == MergeStep.Done) {
                            Text(
                                stringResource(R.string.merge_done_body, moved, dest.displayName),
                                textAlign = TextAlign.Center,
                                modifier = Modifier.padding(top = Space.m),
                            )
                        }
                    }
                },
                confirmButton = {
                    if (step == MergeStep.Done) TextButton(onClick = { onDone(dest.hash) }) { Text(stringResource(R.string.merge_open)) }
                },
            )
        }
    }
}

/** Old avatar on the left, new on the right; dots stream across, then the old one flies in. */
@Composable
private fun MergeAnimation(from: Contact, into: Contact, fly: Float, running: Boolean) {
    val gap = 120.dp
    val t = rememberInfiniteTransition(label = "merge")
    val stream by t.animateFloat(0f, 1f, infiniteRepeatable(tween(900, easing = LinearEasing), RepeatMode.Restart), label = "dots")
    val dot = MaterialTheme.colorScheme.primary
    Box(Modifier.size(width = gap + 64.dp, height = 80.dp), contentAlignment = Alignment.CenterStart) {
        if (running || fly < 1f) {
            for (i in 0 until 3) {
                val p = (stream + i / 3f) % 1f
                Box(
                    Modifier.offset(x = 56.dp + (gap - 48.dp) * p).size(8.dp)
                        .alpha((1f - fly) * (1f - kotlin.math.abs(p - 0.5f) * 1.6f).coerceIn(0f, 1f))
                        .background(dot, CircleShape),
                )
            }
        }
        Box(Modifier.offset(x = gap).size(64.dp).scale(1f + 0.12f * kotlin.math.sin(Math.PI * fly).toFloat()), contentAlignment = Alignment.Center) {
            Avatar(into, size = 64.dp)
            if (fly >= 1f) {
                Box(
                    Modifier.align(Alignment.BottomEnd).size(22.dp).background(MaterialTheme.colorScheme.primary, CircleShape),
                    contentAlignment = Alignment.Center,
                ) { Icon(Icons.Filled.Check, null, tint = MaterialTheme.colorScheme.onPrimary, modifier = Modifier.size(16.dp)) }
            }
        }
        if (fly < 1f) {
            Avatar(
                from, size = 64.dp,
                modifier = Modifier.offset(x = gap * fly).scale(1f - 0.6f * fly).alpha(1f - fly),
            )
        }
    }
}
