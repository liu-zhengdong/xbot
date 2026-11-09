// Keep the catalog order and seed algorithm stable: saved seeds identify faces.
(() => {
  "use strict";
  const slots = {
    hair: ["01-spiky", "02-sidepart", "03-crew", "04-curly", "05-wavy", "06-ponytail", "07-spacebuns", "08-buzz", "09-bob", "10-curtain"],
    tops: ["01-turtleneck-black", "02-turtleneck-cream", "03-blazer", "04-navy-crew", "05-cardigan", "06-hoodie", "07-olive", "08-vest", "09-blue-shirt", "10-suit"],
    face: ["01-neutral", "02-determined", "03-blank", "04-tired", "05-alert", "06-angry", "07-sad", "08-starry", "09-happy", "10-shocked"],
    glasses: ["", "02-round-glasses", "03-square-glasses", "04-sunglasses-forehead"],
    headwear: ["", "05-beret", "06-hardhat", "07-headset", "08-beanie"],
    chest: ["", "09-lapel-pin", "10-chain"],
    props: ["", "01-pen", "02-notebook", "03-mug", "04-laptop", "05-tablet", "06-book", "07-wrench", "08-phone", "09-palette"],
  };
  function make(seed, small) {
    let h = 2166136261;
    for (const ch of String(seed)) { h ^= ch.codePointAt(0); h = Math.imul(h, 16777619) >>> 0; }
    const parts = {};
    for (const [slot, choices] of Object.entries(slots)) {
      h += 0x6D2B79F5;
      let t = Math.imul(h ^ h >>> 15, 1 | h);
      t ^= t + Math.imul(t ^ t >>> 7, 61 | t);
      parts[slot] = choices[((t ^ t >>> 14) >>> 0) % choices.length];
    }
    const layers = ["base/bald", "tops/" + parts.tops];
    if (parts.chest) layers.push("accessories/" + parts.chest);
    layers.push("base/head", "face/" + parts.face, "hair/" + parts.hair);
    if (parts.glasses) layers.push("accessories/" + parts.glasses);
    if (parts.headwear) layers.push("accessories/" + parts.headwear);
    if (parts.props) layers.push("props/" + parts.props);
    // SVG image elements load PNG layers only when mounted in the page DOM.
    return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="' + (small ? '130 50 780 780' : '0 0 1024 1024') + '" data-kind="modular">' +
      layers.map(part => '<image href="/api/ui/static/avatar-parts/' + part + '.png" width="1024" height="1024"/>').join("") + '</svg>';
  }
  window.XBotAvatar = Object.freeze({ portrait: seed => make(seed, false), face: seed => make(seed, true) });
})();
