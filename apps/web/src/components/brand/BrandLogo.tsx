const brandLogoSrc = "/logo.png";

export function BrandLogo({ className = "brand-mark" }: { className?: string }) {
  return (
    <span className={className} aria-hidden="true">
      <img src={brandLogoSrc} alt="" />
    </span>
  );
}
