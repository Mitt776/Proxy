package io.github.mitt776.mitm

import android.graphics.Color
import android.os.Bundle
import android.util.Log
import android.view.Gravity
import android.view.ViewGroup
import android.widget.FrameLayout
import android.widget.TextView
import androidx.activity.ComponentActivity
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import io.github.mitt776.mobile.Mobile
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Сканер QR-кода камерой.
 *
 * Распознаёт не Android, а наше же Go-ядро (backend/appcore/qr.go): тот самый
 * код, что читает QR из картинки на Windows. Так у обеих платформ один
 * распознаватель, и ML Kit с его зависимостью от сервисов Google в APK не
 * попадает.
 *
 * Кадр уезжает в Go плоскостью яркости как есть — без кодирования в JPEG по
 * дороге (см. Mobile.importQRFrame).
 */
class QrScanActivity : ComponentActivity() {

    private val analysisExecutor = Executors.newSingleThreadExecutor()

    /** Найденный код обрабатываем один раз: анализатор успевает выдать ещё кадры. */
    private val done = AtomicBoolean(false)

    /** Момент прошлого разбора: анализировать все 30 кадров в секунду незачем. */
    @Volatile
    private var lastAnalyzed = 0L

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        val root = FrameLayout(this)
        root.setBackgroundColor(Color.BLACK)

        val preview = PreviewView(this)
        preview.layoutParams = FrameLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            ViewGroup.LayoutParams.MATCH_PARENT,
        )
        root.addView(preview)

        val hint = TextView(this)
        hint.text = if (isRussian()) "Наведите камеру на QR-код" else "Point the camera at a QR code"
        hint.setTextColor(Color.WHITE)
        hint.textSize = 15f
        hint.setPadding(48, 48, 48, 96)
        hint.layoutParams = FrameLayout.LayoutParams(
            ViewGroup.LayoutParams.MATCH_PARENT,
            ViewGroup.LayoutParams.WRAP_CONTENT,
            Gravity.BOTTOM or Gravity.CENTER_HORIZONTAL,
        )
        hint.gravity = Gravity.CENTER
        root.addView(hint)

        setContentView(root)
        startCamera(preview)
    }

    private fun startCamera(preview: PreviewView) {
        val future = ProcessCameraProvider.getInstance(this)
        future.addListener({
            val provider = runCatching { future.get() }.getOrNull()
            if (provider == null) {
                finishWith("")
                return@addListener
            }

            val previewUseCase = Preview.Builder().build()
            previewUseCase.surfaceProvider = preview.surfaceProvider

            val analysis = ImageAnalysis.Builder()
                // Копить очередь кадров бессмысленно: пока разбирается старый,
                // пользователь уже сдвинул телефон.
                .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                .build()
            analysis.setAnalyzer(analysisExecutor, ::analyze)

            runCatching {
                provider.unbindAll()
                provider.bindToLifecycle(
                    this,
                    CameraSelector.DEFAULT_BACK_CAMERA,
                    previewUseCase,
                    analysis,
                )
            }.onFailure {
                Log.e(TAG, "камера не открылась", it)
                finishWith("")
            }
        }, mainExecutor)
    }

    private fun analyze(image: ImageProxy) {
        try {
            if (done.get()) return
            val now = System.currentTimeMillis()
            if (now - lastAnalyzed < ANALYZE_EVERY_MS) return
            lastAnalyzed = now

            val plane = image.planes[0]
            val buffer = plane.buffer
            val bytes = ByteArray(buffer.remaining())
            buffer.get(bytes)

            val name = runCatching {
                Mobile.importQRFrame(
                    bytes,
                    image.width,
                    image.height,
                    plane.rowStride,
                )
            }.getOrNull()

            // Пусто — в кадре просто нет кода; это обычное состояние, а не сбой.
            if (!name.isNullOrEmpty() && done.compareAndSet(false, true)) {
                runOnUiThread { finishWith(name) }
            }
        } finally {
            image.close()
        }
    }

    private fun finishWith(profileName: String) {
        if (profileName.isNotEmpty()) {
            setResult(RESULT_OK, intent.putExtra(EXTRA_PROFILE, profileName))
        } else {
            setResult(RESULT_CANCELED)
        }
        finish()
    }

    override fun onDestroy() {
        analysisExecutor.shutdown()
        super.onDestroy()
    }

    private fun isRussian(): Boolean =
        runCatching { Mobile.currentLang() }.getOrDefault("ru") != "en"

    companion object {
        const val EXTRA_PROFILE = "profile"
        private const val TAG = "MitM"

        /** Четыре кадра в секунду: распознавание тяжелее, чем съёмка. */
        private const val ANALYZE_EVERY_MS = 250L
    }
}
