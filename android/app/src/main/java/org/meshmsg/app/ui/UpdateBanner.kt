package org.meshmsg.app.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import org.meshmsg.app.R
import org.meshmsg.app.ui.theme.Space

/** What the chat list's update banner shows (roadmap B7). */
data class UpdateBannerState(
    val version: String,
    val current: String,
    val notes: String,
    /** Below the release's minimum supported version: no "Later". */
    val required: Boolean,
    val downloading: Boolean = false,
    val error: String? = null,
    val onUpdate: () -> Unit = {},
    val onLater: () -> Unit = {},
)

@Composable
fun UpdateBanner(s: UpdateBannerState, modifier: Modifier = Modifier) {
    Card(
        modifier.fillMaxWidth().padding(horizontal = Space.l, vertical = Space.s),
        colors = CardDefaults.cardColors(
            containerColor = if (s.required) MaterialTheme.colorScheme.errorContainer else MaterialTheme.colorScheme.secondaryContainer,
        ),
    ) {
        Column(Modifier.padding(Space.l), verticalArrangement = Arrangement.spacedBy(Space.s)) {
            Text(
                stringResource(if (s.required) R.string.update_required_title else R.string.update_available_title, s.version),
                style = MaterialTheme.typography.titleMedium,
            )
            Text(
                if (s.required) stringResource(R.string.update_required_body, s.current) else stringResource(R.string.update_current, s.current),
                style = MaterialTheme.typography.bodyMedium,
            )
            if (s.notes.isNotBlank()) {
                Text(s.notes, style = MaterialTheme.typography.bodyMedium, maxLines = 6, overflow = TextOverflow.Ellipsis)
            }
            if (s.downloading) LinearProgressIndicator(Modifier.fillMaxWidth())
            s.error?.let { Text(stringResource(R.string.update_failed, it), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
            Row(horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                Button(onClick = s.onUpdate, enabled = !s.downloading, modifier = Modifier.heightIn(min = 48.dp)) {
                    Text(stringResource(if (s.downloading) R.string.update_downloading else R.string.update_now))
                }
                if (!s.required) {
                    TextButton(onClick = s.onLater, modifier = Modifier.heightIn(min = 48.dp)) { Text(stringResource(R.string.update_later)) }
                }
            }
        }
    }
}
