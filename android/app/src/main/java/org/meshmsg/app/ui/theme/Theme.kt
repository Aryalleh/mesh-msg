package org.meshmsg.app.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// Tokens from android/DESIGN.md. Screens use MaterialTheme.colorScheme and
// MeshTheme.extra only — no colour literals outside this file.

private val Dark = darkColorScheme(
    background = Color(0xFF0B1120),
    onBackground = Color(0xFFF1F5F9),
    surface = Color(0xFF0F172A),
    onSurface = Color(0xFFF1F5F9),
    surfaceVariant = Color(0xFF192134),
    onSurfaceVariant = Color(0xFF94A3B8),
    surfaceContainerLowest = Color(0xFF0B1120),
    surfaceContainerLow = Color(0xFF0F172A),
    surfaceContainer = Color(0xFF192134),
    surfaceContainerHigh = Color(0xFF232D45),
    surfaceContainerHighest = Color(0xFF2B3753),
    outline = Color(0xFF33415C),
    outlineVariant = Color(0xFF232D45),
    primary = Color(0xFF8AB4FF),
    onPrimary = Color(0xFF0A1A3D),
    primaryContainer = Color(0xFF1E3A8A),
    onPrimaryContainer = Color(0xFFE3ECFF),
    secondary = Color(0xFF8AB4FF),
    onSecondary = Color(0xFF0A1A3D),
    secondaryContainer = Color(0xFF232D45),
    onSecondaryContainer = Color(0xFFF1F5F9),
    tertiary = Color(0xFF4ADE80),
    onTertiary = Color(0xFF052E16),
    error = Color(0xFFF87171),
    onError = Color(0xFF450A0A),
    errorContainer = Color(0xFF5C1616),
    onErrorContainer = Color(0xFFFFE1E1),
    scrim = Color(0xFF000000),
)

private val Light = lightColorScheme(
    background = Color(0xFFFFFFFF),
    onBackground = Color(0xFF0F172A),
    surface = Color(0xFFFFFFFF),
    onSurface = Color(0xFF0F172A),
    surfaceVariant = Color(0xFFF1F5FD),
    onSurfaceVariant = Color(0xFF526077),
    surfaceContainerLowest = Color(0xFFFFFFFF),
    surfaceContainerLow = Color(0xFFF8FAFF),
    surfaceContainer = Color(0xFFF1F5FD),
    surfaceContainerHigh = Color(0xFFE4ECFC),
    surfaceContainerHighest = Color(0xFFD9E3F8),
    outline = Color(0xFFC5D2EA),
    outlineVariant = Color(0xFFE4ECFC),
    primary = Color(0xFF2563EB),
    onPrimary = Color(0xFFFFFFFF),
    primaryContainer = Color(0xFFDBE7FF),
    onPrimaryContainer = Color(0xFF0B2A6B),
    secondary = Color(0xFF2563EB),
    onSecondary = Color(0xFFFFFFFF),
    secondaryContainer = Color(0xFFE4ECFC),
    onSecondaryContainer = Color(0xFF0F172A),
    tertiary = Color(0xFF047857),
    onTertiary = Color(0xFFFFFFFF),
    error = Color(0xFFDC2626),
    onError = Color(0xFFFFFFFF),
    errorContainer = Color(0xFFFEE2E2),
    onErrorContainer = Color(0xFF7F1D1D),
    scrim = Color(0xFF000000),
)

/** Semantic colours Material 3 has no slot for. */
@Immutable
data class ExtraColors(
    val success: Color,
    val warning: Color,
    val avatars: List<Color>,
    val onAvatar: Color,
    /** The second tick once the contact has read the message (on the outgoing bubble). */
    val read: Color = Color(0xFF5EEAD4),
)

private val DarkExtra = ExtraColors(
    success = Color(0xFF4ADE80),
    warning = Color(0xFFFBBF24),
    // Muted tones with ≥ 4.5:1 against onAvatar.
    avatars = listOf(
        Color(0xFF1D4ED8), Color(0xFF0F766E), Color(0xFF7C3AED), Color(0xFFB45309),
        Color(0xFFBE185D), Color(0xFF15803D), Color(0xFF4338CA), Color(0xFF0369A1),
    ),
    onAvatar = Color(0xFFFFFFFF),
)

private val LightExtra = DarkExtra.copy(success = Color(0xFF047857), warning = Color(0xFFB45309), read = Color(0xFF0F766E))

private val LocalExtra = staticCompositionLocalOf { DarkExtra }

private val Type = Typography().run {
    copy(
        titleLarge = TextStyle(fontSize = 22.sp, lineHeight = 28.sp, fontWeight = FontWeight.SemiBold),
        titleMedium = TextStyle(fontSize = 16.sp, lineHeight = 24.sp, fontWeight = FontWeight.SemiBold),
        bodyLarge = TextStyle(fontSize = 16.sp, lineHeight = 24.sp, fontWeight = FontWeight.Normal),
        bodyMedium = TextStyle(fontSize = 14.sp, lineHeight = 21.sp, fontWeight = FontWeight.Normal),
        labelLarge = TextStyle(fontSize = 14.sp, lineHeight = 20.sp, fontWeight = FontWeight.Medium),
        labelMedium = TextStyle(fontSize = 12.sp, lineHeight = 16.sp, fontWeight = FontWeight.Medium),
        labelSmall = TextStyle(fontSize = 12.sp, lineHeight = 16.sp, fontWeight = FontWeight.Medium, fontFeatureSettings = "tnum"),
    )
}

private val MeshShapes = Shapes(
    extraSmall = RoundedCornerShape(4.dp),
    small = RoundedCornerShape(8.dp),
    medium = RoundedCornerShape(16.dp),
    large = RoundedCornerShape(20.dp),
    extraLarge = RoundedCornerShape(28.dp),
)

object MeshTheme {
    val extra: ExtraColors @Composable get() = LocalExtra.current

    /** Hashes and links. */
    val mono: TextStyle
        @Composable get() = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace)
}

/** Spacing on the 4 dp grid. */
object Space {
    val xs = 4.dp
    val s = 8.dp
    val m = 12.dp
    val l = 16.dp
    val xl = 24.dp
    val xxl = 32.dp
}

/** Dark is the default (android/DESIGN.md); [dark] follows the system unless the user chose. */
@Composable
fun MeshMsgTheme(dark: Boolean = isSystemInDarkTheme(), content: @Composable () -> Unit) {
    CompositionLocalProvider(LocalExtra provides if (dark) DarkExtra else LightExtra) {
        MaterialTheme(colorScheme = if (dark) Dark else Light, typography = Type, shapes = MeshShapes, content = content)
    }
}
