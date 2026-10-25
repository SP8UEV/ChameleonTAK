import { useEffect, useState } from 'react';
import QRCode from 'qrcode';

interface QrCodeProps {
  value: string;
  /** Szerokość w px (CSS); SVG skaluje się bez utraty jakości. */
  size?: number;
  label?: string;
  className?: string;
}

/** Kod QR renderowany po stronie przeglądarki (SVG, zawsze czarny na białym). */
export function QrCode({ value, size = 220, label, className }: QrCodeProps) {
  const [svg, setSvg] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let alive = true;
    setFailed(false);
    if (!value) {
      setSvg(null);
      return;
    }
    QRCode.toString(value, {
      type: 'svg',
      errorCorrectionLevel: value.length > 300 ? 'L' : 'M',
      margin: 1,
      color: { dark: '#000000', light: '#ffffff' },
    })
      .then((s) => {
        if (alive) setSvg(s);
      })
      .catch(() => {
        if (alive) setFailed(true);
      });
    return () => {
      alive = false;
    };
  }, [value]);

  return (
    <div
      className={`qr${className ? ` ${className}` : ''}`}
      style={{ width: size, maxWidth: '100%' }}
      role="img"
      aria-label={label ?? 'Kod QR'}
    >
      {failed || !value ? (
        <div className="qr-error">{value ? 'Nie udało się wygenerować kodu QR' : 'Brak kodu'}</div>
      ) : svg ? (
        <div dangerouslySetInnerHTML={{ __html: svg }} />
      ) : (
        <div className="qr-error" />
      )}
    </div>
  );
}
