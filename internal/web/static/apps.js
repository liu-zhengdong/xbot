(() => {
  let teardown = () => {};
  const node = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ""; if (text != null) n.textContent = text; return n; };
  const button = (cls, text, action) => { const b = node("button", cls, text); b.type = "button"; b.onclick = action; return b; };
  const plain = (e, fallback) => e && e.status >= 400 && e.status < 500 && /[一-鿿]/.test(e.message || "") ? e.message : fallback;
  const CHIPS = [["all", "全部"], ["dev", "开发"], ["work", "效率"], ["chat", "通讯"], ["more", "更多"]];
  const FEATURED = ["playwright", "context7"];
  // Brand marks: Iconify logos and Semi Design Feishu; custom apps get the plug.
  const ICONS = {
    playwright: `<svg viewBox="0 0 256 192" xmlns="http://www.w3.org/2000/svg"><path fill="#2d4552" d="M84.38 108.352c-9.556 2.712-15.826 7.467-19.956 12.218c3.956-3.461 9.255-6.639 16.402-8.665c7.311-2.072 13.548-2.057 18.702-1.062v-4.03c-4.397-.402-9.437-.082-15.148 1.539M63.987 74.475l-35.49 9.35s.646.914 1.844 2.133l30.092-7.93s-.427 5.495-4.13 10.41c7.005-5.299 7.684-13.963 7.684-13.963m29.709 83.41c-49.946 13.452-76.37-44.43-84.37-74.472c-3.696-13.868-5.31-24.37-5.74-31.148a11.5 11.5 0 0 1 .025-1.84C1.021 50.58-.22 51.927.032 55.82c.43 6.773 2.044 17.275 5.74 31.147c7.997 30.038 34.424 87.92 84.37 74.468c10.871-2.929 19.038-8.263 25.17-15.073c-5.652 5.104-12.724 9.123-21.616 11.523M103.08 39.05v3.555h19.59c-.401-1.259-.806-2.393-1.208-3.555z"/><path fill="#2d4552" d="M127.05 68.325c8.81 2.503 13.47 8.68 15.933 14.146l9.824 2.79s-1.34-19.132-18.645-24.047c-16.189-4.6-26.151 8.995-27.363 10.754c4.71-3.355 11.586-6.102 20.251-3.643m78.197 14.234c-16.204-4.62-26.162 9.003-27.356 10.737c4.713-3.351 11.586-6.099 20.247-3.629c8.797 2.506 13.452 8.676 15.923 14.146l9.837 2.8s-1.361-19.135-18.651-24.054m-9.76 50.443l-81.718-22.845s.885 4.485 4.279 10.293l68.803 19.234c5.664-3.277 8.636-6.682 8.636-6.682m-56.655 49.174C74.127 164.828 81.949 82.386 92.419 43.32c4.311-16.1 8.743-28.066 12.419-36.088c-2.193-.451-4.01.704-5.804 4.354C95.13 19.5 90.14 32.387 85.312 50.427c-10.467 39.066-18.29 121.506 46.412 138.854c30.497 8.17 54.256-4.247 71.966-23.749c-16.81 15.226-38.274 23.763-64.858 16.644"/><path fill="#e2574c" d="M103.081 138.565v-16.637l-46.223 13.108s3.415-19.846 27.522-26.684c7.311-2.072 13.549-2.058 18.701-1.063V39.05h23.145c-2.52-7.787-4.958-13.782-7.006-17.948c-3.387-6.895-6.859-2.324-14.741 4.269c-5.552 4.638-19.583 14.533-40.698 20.222c-21.114 5.694-38.185 4.184-45.307 2.95c-10.097-1.742-15.378-3.96-14.884 3.721c.43 6.774 2.043 17.277 5.74 31.148c7.996 30.039 34.424 87.92 84.37 74.468c13.046-3.515 22.254-10.464 28.637-19.32h-19.256zm-74.588-54.74l35.494-9.35s-1.034 13.654-14.34 17.162c-13.31 3.504-21.154-7.812-21.154-7.812"/><path fill="#2ead33" d="M236.664 39.84c-9.226 1.617-31.361 3.632-58.716-3.7c-27.363-7.328-45.517-20.144-52.71-26.168c-10.197-8.54-14.682-14.476-19.096-5.498c-3.902 7.918-8.893 20.805-13.723 38.846c-10.466 39.066-18.289 121.505 46.413 138.853c64.687 17.333 99.126-57.978 109.593-97.047c4.83-18.037 6.948-31.695 7.53-40.502c.665-9.976-6.187-7.08-19.29-4.784M106.668 72.161s10.196-15.859 27.49-10.943c17.305 4.915 18.645 24.046 18.645 24.046zm42.215 71.163c-30.419-8.91-35.11-33.167-35.11-33.167l81.714 22.846c0-.004-16.494 19.12-46.604 10.32m28.89-49.85s10.183-15.847 27.474-10.918c17.29 4.923 18.651 24.054 18.651 24.054z"/><path fill="#d65348" d="m86.928 126.51l-30.07 8.522s3.266-18.609 25.418-25.983L65.25 45.147l-1.471.447c-21.115 5.694-38.185 4.184-45.307 2.95c-10.097-1.741-15.379-3.96-14.885 3.722c.43 6.774 2.044 17.276 5.74 31.147c7.997 30.039 34.425 87.92 84.37 74.468l1.471-.462zM28.493 83.825l35.494-9.351s-1.034 13.654-14.34 17.162c-13.31 3.504-21.154-7.811-21.154-7.811"/><path fill="#1d8d22" d="m150.255 143.658l-1.376-.335c-30.419-8.91-35.11-33.166-35.11-33.166l42.137 11.778l22.308-85.724l-.27-.07c-27.362-7.329-45.516-20.145-52.71-26.17c-10.196-8.54-14.682-14.475-19.096-5.497c-3.898 7.918-8.889 20.805-13.719 38.846c-10.466 39.066-18.289 121.505 46.413 138.852l1.326.3zM106.668 72.16s10.196-15.859 27.49-10.943c17.305 4.915 18.645 24.046 18.645 24.046z"/><path fill="#c04b41" d="m88.46 126.072l-8.064 2.289c1.906 10.74 5.264 21.047 10.534 30.152c.918-.202 1.828-.376 2.762-.632c2.449-.66 4.72-1.479 6.906-2.371c-5.89-8.74-9.785-18.804-12.137-29.438m-3.148-75.644c-4.144 15.467-7.852 37.73-6.831 60.06c1.826-.793 3.756-1.532 5.9-2.14l1.492-.334c-1.82-23.852 2.114-48.157 6.546-64.694a323 323 0 0 1 3.373-11.704a105 105 0 0 1-5.974 3.547a307 307 0 0 0-4.506 15.265"/></svg>`,
    context7: `<svg viewBox="0 0 24 24" fill="none" stroke="#5b5bd6" stroke-width="1.7" stroke-linecap="round"><path d="M12 3 4 7v10l8 4 8-4V7l-8-4Z"/><path d="M12 12 4 7"/><path d="m12 12 8-5"/><path d="M12 12v10"/></svg>`,
    telegram: `<svg viewBox="0 0 256 256" xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="SVG6DaOZcwt" x1="50%" x2="50%" y1="0%" y2="100%"><stop offset="0%" stop-color="#2aabee"/><stop offset="100%" stop-color="#229ed9"/></linearGradient></defs><path fill="url(#SVG6DaOZcwt)" d="M128 0C94.06 0 61.48 13.494 37.5 37.49A128.04 128.04 0 0 0 0 128c0 33.934 13.5 66.514 37.5 90.51C61.48 242.506 94.06 256 128 256s66.52-13.494 90.5-37.49c24-23.996 37.5-56.576 37.5-90.51s-13.5-66.514-37.5-90.51C194.52 13.494 161.94 0 128 0"/><path fill="#fff" d="M57.94 126.648q55.98-24.384 74.64-32.152c35.56-14.786 42.94-17.354 47.76-17.441c1.06-.017 3.42.245 4.96 1.49c1.28 1.05 1.64 2.47 1.82 3.467c.16.996.38 3.266.2 5.038c-1.92 20.24-10.26 69.356-14.5 92.026c-1.78 9.592-5.32 12.808-8.74 13.122c-7.44.684-13.08-4.912-20.28-9.63c-11.26-7.386-17.62-11.982-28.56-19.188c-12.64-8.328-4.44-12.906 2.76-20.386c1.88-1.958 34.64-31.748 35.26-34.45c.08-.338.16-1.598-.6-2.262c-.74-.666-1.84-.438-2.64-.258c-1.14.256-19.12 12.152-54 35.686c-5.1 3.508-9.72 5.218-13.88 5.128c-4.56-.098-13.36-2.584-19.9-4.708c-8-2.606-14.38-3.984-13.82-8.41c.28-2.304 3.46-4.662 9.52-7.072"/></svg>`,
    lark: `<svg width="24" height="24" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M6.13732 3.80654C8.76716 5.98777 11.0232 8.49408 12.7428 11.4327L14.4397 9.75535C15.2458 8.96545 16.2113 8.34129 17.258 7.93496C16.7802 6.31936 16.0033 4.98005 14.9403 3.65376C14.8135 3.49449 14.6185 3.40346 14.4137 3.40346L6.28362 3.40021C6.06906 3.40021 5.9748 3.67001 6.13732 3.80654Z" fill="#3370FF"/><path d="M20.5703 14.1922L20.58 14.1793L20.6155 14.1146C20.6026 14.1372 20.5864 14.1631 20.5703 14.1922Z" fill="#3370FF"/><path d="M11.0361 14.5567C12.2714 15.0833 13.3766 15.5287 14.6899 15.883C17.0207 16.5136 19.2311 15.5709 20.3234 13.4872L21.6432 10.8541C21.939 10.2105 22.3128 9.6156 22.7647 9.07273C21.9715 8.78016 21.3149 8.63064 20.4534 8.63064C18.5518 8.63064 16.7606 9.36205 15.4018 10.6948L13.3831 12.6875C12.6647 13.3929 11.878 14.0203 11.0361 14.5567Z" fill="#3370FF"/><path d="M20.7945 13.7752L20.8039 13.7566L20.8101 13.7473C20.8039 13.7535 20.8007 13.7659 20.7945 13.7752Z" fill="#3370FF"/><path d="M1.62519 9.74885C1.47889 9.60906 1.23511 9.70983 1.23511 9.91463L1.23834 17.8724C1.23834 18.1 1.34887 18.3112 1.53416 18.4348C3.66663 19.8521 6.15343 20.5998 8.72151 20.5998C11.166 20.5998 13.5488 19.9171 15.6098 18.6266C16.3867 18.139 16.9979 17.7099 17.6512 17.0792C16.6013 17.388 15.4472 17.4075 14.2835 17.0922C9.37815 15.7594 5.20097 13.2044 1.62519 9.74885Z" fill="#3370FF"/></svg>`,
    gmail: `<svg viewBox="0 0 256 204" xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="SVGHzDbHcwG" x1="165" x2="165" y1="44" y2="166" gradientUnits="userSpaceOnUse"><stop stop-color="#60d673"/><stop offset=".17" stop-color="#42c868"/><stop offset=".39" stop-color="#0ebc5f"/><stop offset=".62" stop-color="#00a9bb"/><stop offset=".86" stop-color="#3c90ff"/><stop offset="1" stop-color="#3186ff"/></linearGradient><linearGradient id="SVG41ATXboZ" x1="8" x2="184" y1="46.13" y2="46.13" gradientUnits="userSpaceOnUse"><stop offset=".08" stop-color="#ff63a0"/><stop offset=".3" stop-color="#fc413d"/><stop offset=".5" stop-color="#fc413d"/><stop offset=".65" stop-color="#fc413d"/><stop offset=".72" stop-color="#fc5c30"/><stop offset=".86" stop-color="#feb10c"/><stop offset=".91" stop-color="#fec700"/><stop offset=".96" stop-color="#ffdb0f"/></linearGradient></defs><path fill="url(#SVGHzDbHcwG)" d="M146 44h38v110c0 6.627-5.373 12-12 12h-20a6 6 0 0 1-6-6z" transform="translate(-11.636 -37.818)scale(1.45454)"/><path fill="#fc413d" d="M55.273 26.182H0v160c0 9.638 7.816 17.454 17.455 17.454h29.09a8.727 8.727 0 0 0 8.728-8.728z"/><path fill="url(#SVG41ATXboZ)" d="M39.226 30.456c-8.033-6.752-20.018-5.714-26.77 2.319c-6.752 8.032-5.714 20.017 2.319 26.77l76.078 63.949a8 8 0 0 0 10.295 0l76.078-63.95c8.032-6.752 9.07-18.737 2.318-26.77c-6.752-8.032-18.737-9.07-26.769-2.318L96 78.18z" transform="translate(-11.636 -37.818)scale(1.45454)"/></svg>`,
    gdrive: `<svg viewBox="0 0 256 238" xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="SVGjzk6ueva" x1="86.924%" x2="7.63%" y1="94.294%" y2="45.952%"><stop offset="9%" stop-color="#ffe921"/><stop offset="100%" stop-color="#fec700"/></linearGradient><linearGradient id="SVGb0f7AcFJ" x1="99.538%" x2="23.437%" y1="93.901%" y2="54.033%"><stop offset="15%" stop-color="#a9a8ff"/><stop offset="33%" stop-color="#6d97ff"/><stop offset="48%" stop-color="#3186ff"/></linearGradient><linearGradient id="SVGxMwz6dYk" x1="87.128%" x2="-.639%" y1="51.518%" y2="93.078%"><stop offset="55%" stop-color="#0ebc5f"/><stop offset="85%" stop-color="#78c9ff"/></linearGradient><path id="SVGAiIt3btS" d="M77.303 29.27c22.531-39.026 78.863-39.027 101.394 0l69.373 120.158c22.531 39.027-5.633 87.81-50.698 87.81H58.628c-45.065 0-73.23-48.783-50.698-87.81z"/></defs><mask id="SVG6FLXsceq" fill="#fff"><use href="#SVGAiIt3btS"/></mask><g mask="url(#SVG6FLXsceq)"><path fill="url(#SVGjzk6ueva)" d="M341.719 295.937H200.166l-29.293-50.735l70.777-122.589z" transform="translate(-42.87 -58.67)"/><path fill="url(#SVGb0f7AcFJ)" d="m0 295.916l100.069-173.325v.003l-29.282 50.723h58.57l70.783 122.596l-200.138.001z" transform="translate(-42.87 -58.67)"/><path fill="url(#SVGxMwz6dYk)" d="m170.881 0l70.781 122.6l-29.286 50.726H70.812z" transform="translate(-42.87 -58.67)"/></g></svg>`,
    gcalendar: `<svg viewBox="0 0 256 275" xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="SVGAUTqNcwQ" x1="83" x2="83" y1="76" gradientUnits="userSpaceOnUse"><stop stop-color="#4fa0ff"/><stop offset="1" stop-color="#3186ff"/></linearGradient><linearGradient id="SVGMJemac7b" x1="89.06" x2="89.06" y1="21.75" y2="96.39" gradientUnits="userSpaceOnUse"><stop stop-color="#a9a8ff"/><stop offset=".8" stop-color="#3c90ff"/></linearGradient><filter id="SVGGQyK4bRj" width="152" height="112" x="20" y="-4" color-interpolation-filters="sRGB" filterUnits="userSpaceOnUse"><feFlood flood-opacity="0" result="BackgroundImageFix"/><feBlend in="SourceGraphic" in2="BackgroundImageFix" result="shape"/><feGaussianBlur result="effect1_foregroundBlur_37330_7673" stdDeviation="6"/></filter></defs><mask id="SVGcTVVObIw" width="154" height="152" x="19" y="20" maskUnits="userSpaceOnUse" style="mask-type:alpha"><path fill="#3c90ff" d="M19.867 49.392C17.818 33.82 29.94 20 45.645 20h100.71c15.706 0 27.827 13.82 25.778 29.392L166 96l6.133 46.608C174.182 158.18 162.061 172 146.355 172H45.645c-15.706 0-27.827-13.82-25.778-29.392L26 96z"/></mask><mask id="SVGBwV4Xb4k" width="154" height="152" x="19" y="20" maskUnits="userSpaceOnUse" style="mask-type:alpha"><path fill="#3186ff" d="M19.867 49.392C17.818 33.82 29.94 20 45.645 20h100.71c15.706 0 27.827 13.82 25.778 29.392L166 96l6.133 46.608C174.182 158.18 162.061 172 146.355 172H45.645c-15.706 0-27.827-13.82-25.778-29.392L26 96z"/></mask><path fill="#bbe2ff" d="M20.824 48.078c0-26.641 21.596-48.239 48.238-48.239h117.917c26.641 0 48.238 21.598 48.238 48.239v50.918c0 26.641-21.597 48.239-48.238 48.239H69.062c-26.642 0-48.238-21.598-48.238-48.239z"/><path fill="#3c90ff" d="M.502 69.169c-3.432-26.082 16.871-49.23 43.177-49.23h168.683c26.306 0 46.609 23.148 43.178 49.23l-10.273 78.066l10.273 78.066c3.431 26.082-16.872 49.23-43.178 49.23H43.679c-26.307 0-46.609-23.148-43.177-49.23l10.272-78.066z"/><g mask="url(#SVGcTVVObIw)" transform="translate(-32.775 -13.56)scale(1.67495)"><path fill="url(#SVGAUTqNcwQ)" d="M0 0h166v76H0z" transform="matrix(1 0 0 -1 13 172)"/></g><g mask="url(#SVGBwV4Xb4k)" transform="translate(-32.775 -13.56)scale(1.67495)"><path fill="url(#SVGMJemac7b)" d="M32 27.2C32 16.596 40.596 8 51.2 8h89.6c10.604 0 19.2 8.596 19.2 19.2V96H32z" filter="url(#SVGGQyK4bRj)"/></g><path fill="#fff" d="M93.438 209.77q-10.521 0-18.051-3.421q-7.528-3.422-12.746-9.153q-5.133-5.82-7.273-11.379q-2.137-5.558-1.711-6.758a3.47 3.47 0 0 1 1.711-1.882l9.497-3.765q1.196-.598 2.395-.17q1.196.34 2.822 3.935q1.714 3.592 4.79 7.613a24 24 0 0 0 7.529 6.245q4.368 2.226 10.779 2.225q10.351 0 16.426-5.99q6.16-5.986 6.16-15.226q0-10.01-6.503-15.4q-6.5-5.476-17.195-5.476h-8.981a3.17 3.17 0 0 1-2.225-.853q-.853-.94-.855-2.139v-9.153q0-1.284.854-2.14a3.06 3.06 0 0 1 2.226-.94h7.783q9.583 0 15.4-5.22q5.817-5.218 5.816-13.517q.002-8.21-5.218-13.258t-14.371-5.049q-5.135.001-8.898 1.712a19.3 19.3 0 0 0-6.502 4.791a38.3 38.3 0 0 0-4.705 6.33q-1.964 3.252-3.165 3.593q-1.195.256-2.31-.426l-8.983-4.364q-1.11-.599-1.454-1.882q-.342-1.284 2.055-5.989q2.48-4.788 7.526-9.752a35.2 35.2 0 0 1 11.807-7.7q6.758-2.74 15.741-2.738q16.681.001 26.434 8.81q9.752 8.73 9.753 23.099q0 9.926-4.79 17.196q-4.706 7.27-13.347 10.268v.341q10.438 3.08 16.425 11.291c4.05 5.421 6.075 11.891 6.074 19.419q0 16.17-11.293 26.522q-11.29 10.35-29.429 10.35zm85.841-1.967q-1.452 0-2.567-1.112a3.78 3.78 0 0 1-1.026-2.652v-95.131l-19.249 13.859q-1.026.77-2.396.514a3.3 3.3 0 0 1-2.052-1.283l-5.561-7.871a3.32 3.32 0 0 1-.6-2.397q.258-1.365 1.369-2.136l34.133-24.382q.43-.343.942-.514q.515-.256 1.197-.256h7.188q1.455.001 2.31 1.027q.941.94.941 2.396v116.174q0 1.542-1.112 2.652a3.35 3.35 0 0 1-2.568 1.112z"/></svg>`,
  };
  const PLUG = `<svg viewBox="0 0 24 24" fill="none" stroke="#71717a" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M9 3v5"/><path d="M15 3v5"/><path d="M6 8h12v4a6 6 0 0 1-12 0V8Z"/><path d="M12 18v3"/></svg>`;
  const icon = a => { const n = node("span", "ico"); n.setAttribute("aria-hidden", "true"); n.innerHTML = ICONS[a.id] || PLUG; return n; };
  const textOf = a => (a.title + " " + a.summary).toLowerCase();

  // The store installs for the owner, so every call here is free of any Bot.
  async function mount({api, host, isCurrent}) {
    teardown();
    let alive = true, apps = [], count = 0, chip = "all", query = "", view = "store", busy = new Set(), layer = null, cardErrors = new Map();
    const current = () => alive && isCurrent();
    let toastTimer = 0;
    teardown = () => { alive = false; clearTimeout(toastTimer); layer?.controller?.abort(); layer = null; host.replaceChildren(); };
    host.className = "skills-settings apps-settings";
    const body = node("div", "ap-body");
    const errorBox = node("p", "sk-error"); errorBox.setAttribute("role", "alert"); errorBox.hidden = true;
    const layers = node("div", "sk-layers");
    // The app-wide toast sits under the settings sheet, so the store shows its own.
    const notice = node("div", "toast"); notice.setAttribute("role", "status"); notice.hidden = true;
    const toast = text => { notice.textContent = text; notice.hidden = false; clearTimeout(toastTimer); toastTimer = setTimeout(() => { notice.hidden = true; }, 3000); };
    host.replaceChildren(body, errorBox, layers, notice);
    const fail = text => { errorBox.textContent = text || ""; errorBox.hidden = !text; };

    async function load() {
      try { const d = await api("/api/apps"); apps = d.apps || []; count = apps.filter(a => a.state === "connected").length; }
      catch { if (current()) fail("没能读取应用，请重新打开试试。"); return false; }
      return current();
    }

    function label(a) {
      if (a.state === "soon") return ["即将支持", "btn ap-soon", true];
      // A connected card can be taken back right here, the same as the row on the connected page.
      if (a.state === "connected") return ["移除", "btn", false];
      return [a.category === "chat" ? "连接" : "添加", "btn pri", false];
    }
    async function add(a) {
      if (busy.has(a.id)) return;
      if (a.card) { openSecretLayer(a); return; }
      busy.add(a.id); cardErrors.delete(a.id); fail(""); paint();
      try { await api("/api/apps", {method: "POST", body: {id: a.id}}); if (!(await load())) return; toast("已添加到你的应用"); }
      catch (e) { if (current()) { const message = plain(e, "没能添加，请再试一次。"); cardErrors.set(a.id, message); fail(message); } }
      finally { busy.delete(a.id); }
      if (current()) paint();
    }
    async function remove(a) {
      if (busy.has(a.id)) return;
      busy.add(a.id); fail("");
      try { await api("/api/apps/" + encodeURIComponent(a.id), {method: "DELETE"}); if (!(await load())) return; }
      catch (e) { if (current()) fail(plain(e, "没能移除，请再试一次。")); }
      finally { busy.delete(a.id); }
      if (current()) paint();
    }

    function card(a) {
      const c = node("div", "ap-card"), tx = node("div", "tx"), [text, cls, off] = label(a);
      tx.append(node("b", "", a.title), node("span", "hint", a.summary));
      if (a.scope) tx.append(node("span", "ap-scope", a.scope));
      if (cardErrors.has(a.id)) tx.append(node("span", "sk-error", cardErrors.get(a.id)));
      const b = button(cls, busy.has(a.id) ? text + "中…" : text, () => a.state === "connected" ? remove(a) : add(a)); b.disabled = off || busy.has(a.id);
      c.append(icon(a), tx, b); return c;
    }
    function paint() { return view === "connected" ? showConnected() : showStore(); }

    function showStore() {
      view = "store";
      const head = node("div", "sethd ap-head"); head.append(node("h2", "", "连接应用"));
      const open = button("ap-count", "已连接 " + count + " 个", () => { view = "connected"; fail(""); paint(); }); open.append(node("span", "skchev"));
      head.append(open);
      const wrap = node("div", "sksearchwrap ap-search"), glyph = node("span", "sksic"); glyph.setAttribute("aria-hidden", "true");
      const search = node("input", "sksearch"); search.type = "search"; search.placeholder = "搜索应用…"; search.setAttribute("aria-label", "搜索应用"); search.autocomplete = "off"; search.value = query;
      wrap.append(glyph, search);
      const chips = node("div", "ap-chips"); chips.setAttribute("role", "group"); chips.setAttribute("aria-label", "分类");
      const grid = node("div", "ap-grid-wrap");
      const store = apps.filter(a => !a.custom);
      function fill() {
        for (const b of chips.children) { const on = b.dataset.chip === chip; b.classList.toggle("on", on); b.setAttribute("aria-pressed", String(on)); }
        const q = query.trim().toLowerCase();
        const rows = store.filter(a => (chip === "all" || a.category === chip) && (!q || textOf(a).includes(q)));
        const parts = [];
        if (!rows.length) {
          const none = node("div", "sknone"); none.append(node("p", "", "没有匹配的应用"), button("clearsrch", "清除搜索", () => { query = ""; chip = "all"; search.value = ""; fill(); }));
          parts.push(none);
        } else if (chip === "all" && !q) {
          const rec = FEATURED.map(id => rows.find(a => a.id === id)).filter(Boolean);
          const section = (title, list) => { const s = node("section", "ap-sec"); s.append(node("h4", "", title)); const g = node("div", "ap-grid"); g.append(...list.map(card)); s.append(g); return s; };
          parts.push(section("为你推荐", rec));
          for (const [id, name] of CHIPS.slice(1, 5)) { const list = rows.filter(a => a.category === id && !rec.includes(a)); if (list.length) parts.push(section(name, list)); }
        } else { const g = node("div", "ap-grid"); g.append(...rows.map(card)); parts.push(g); }
        grid.replaceChildren(...parts);
      }
      for (const [id, name] of CHIPS) { const b = button("ap-chip", name, () => { chip = id; fill(); }); b.dataset.chip = id; chips.append(b); }
      search.oninput = () => { query = search.value; fill(); };
      const foot = node("p", "setfoot ap-foot"); foot.append(button("btn linkish", "自定义应用…", ev => openCustom(ev.currentTarget)));
      fill();
      body.replaceChildren(head, wrap, chips, grid, foot);
    }

    function showConnected() {
      const back = button("setback", "", () => { view = "store"; fail(""); paint(); });
      back.append(node("span", "skback-chev"), document.createTextNode("返回商店"));
      const head = node("div", "sethd"); head.append(node("h2", "", "已连接 " + count + " 个"));
      const list = apps.filter(a => a.state === "connected");
      const lead = node("p", "setlead", list.some(a => a.scope) ? "多数应用对所有 Bot 都能用；带说明的，只有填过密钥的 Bot 能用。" : "这些应用对所有 Bot 都能用。");
      if (!list.length) {
        const empty = node("div", "skempty"), art = node("span", "skempty-art"); art.setAttribute("aria-hidden", "true");
        empty.append(art, node("h3", "", "还没有连接应用"), node("p", "", "到商店里添加一个，所有 Bot 都能用。"), button("btn pri", "去商店看看", () => { view = "store"; fail(""); paint(); }));
        body.replaceChildren(back, head, empty); return;
      }
      const rows = node("div", "sklist");
      rows.append(...list.map(a => {
        const row = node("div", "skrow ap-row"), tx = node("span", "tx");
        tx.append(node("b", "", a.title), node("span", "hint", a.summary));
        if (a.scope) tx.append(node("span", "ap-scope", a.scope));
        const b = button("btn", "移除", () => remove(a)); b.disabled = busy.has(a.id);
        row.append(icon(a), tx, b); return row;
      }));
      body.replaceChildren(back, head, lead, rows);
    }

    function closeLayer() { if (!layer) return; const {opener, controller} = layer; controller?.abort(); layer = null; layers.replaceChildren(); opener?.isConnected && opener.focus?.({preventScroll: true}); }
    function openSecretLayer(a) {
      if (layer) closeLayer();
      const root = node("div", "nestlayer"), cardEl = node("div", "nestcard");
      const action = a.category === "chat" ? "连接" : "添加";
      root.setAttribute("role", "dialog"); root.setAttribute("aria-modal", "true"); root.setAttribute("aria-label", action + " " + a.title);
      const labels = a.fields?.length ? a.fields : ["密钥"];
      const fields = labels.map(label => { const wrap = node("label", "ap-field"), input = node("input"); input.type = "password"; input.autocomplete = "off"; input.placeholder = "粘贴" + label; wrap.append(node("span", "", label), input); return [wrap, input]; });
      const err = node("p", "sk-error"); err.hidden = true; err.setAttribute("role", "alert");
      const ok = button("btn pri", action, async () => {
        if (ok.disabled) return;
        const missing = fields.findIndex(([, input]) => !input.value.trim());
        if (missing >= 0) { err.textContent = "请填写" + labels[missing] + "。"; err.hidden = false; fields[missing][1].focus(); return; }
        err.hidden = true; ok.disabled = true; ok.textContent = action + "中…";
        const controller = new AbortController(); layer.controller = controller;
        try {
          await api("/api/apps", {method: "POST", body: {id: a.id, secrets: fields.map(([, input]) => input.value)}, signal: controller.signal});
          if (!current() || controller.signal.aborted) return;
          layer.controller = null; closeLayer(); if (!(await load())) return; paint(); toast("已添加到你的应用");
        } catch (e) { if (controller.signal.aborted || !current()) return; err.textContent = "密钥无效或连不上，请检查后重试。"; err.hidden = false; ok.disabled = false; ok.textContent = action; }
      });
      const actions = node("div", "nest-actions"); actions.append(button("btn", "取消", closeLayer), ok);
      cardEl.append(node("h3", "", action + " " + a.title), node("p", "lead", "需要填写下面的密钥才能用。只保存在本机，不会出现在对话里。"), ...fields.map(([wrap]) => wrap), err, actions);
      root.append(cardEl); layers.replaceChildren(root); layer = {opener: null};
      root.addEventListener("keydown", ev => { if (ev.key === "Escape") { ev.preventDefault(); ev.stopPropagation(); closeLayer(); } });
      fields[0][1].focus({preventScroll: true});
    }

    function openCustom(opener) {
      if (layer) closeLayer();
      const root = node("div", "nestlayer"), cardEl = node("div", "nestcard");
      root.setAttribute("role", "dialog"); root.setAttribute("aria-modal", "true"); root.setAttribute("aria-label", "自定义应用");
      let mode = "program";
      const field = (labelText, ph, multi) => { const w = node("label", "ap-field"); w.append(node("span", "", labelText)); const i = node(multi ? "textarea" : "input"); if (!multi) i.type = "text"; i.placeholder = ph || ""; i.autocomplete = "off"; w.append(i); return [w, i]; };
      const [nameW, name] = field("名称", "比如：我的笔记");
      const [progW, prog] = field("本机程序", "程序的名称或位置");
      const [optW, opts] = field("附加选项（每行一个，可不填）", "", true);
      const [urlW, url] = field("网址", "https://");
      const tabs = node("div", "ap-chips");
      const setMode = m => { mode = m; progW.hidden = optW.hidden = m !== "program"; urlW.hidden = m !== "url"; for (const b of tabs.children) { b.classList.toggle("on", b.dataset.m === m); b.setAttribute("aria-pressed", String(b.dataset.m === m)); } };
      for (const [m, t] of [["program", "本机程序"], ["url", "网址"]]) { const b = button("ap-chip", t, () => setMode(m)); b.dataset.m = m; tabs.append(b); }
      const err = node("p", "sk-error"); err.hidden = true; err.setAttribute("role", "alert");
      const ok = button("btn pri", "添加", async () => {
        err.hidden = true; ok.disabled = true;
        const bodyJSON = mode === "program" ? {name: name.value, program: prog.value, options: opts.value.split("\n").map(s => s.trim()).filter(Boolean)} : {name: name.value, url: url.value};
        try { await api("/api/apps/custom", {method: "POST", body: bodyJSON}); if (!current()) return; closeLayer(); await load(); if (current()) paint(); }
        catch (e) { err.textContent = plain(e, "没能添加，请检查填写的内容。"); err.hidden = false; ok.disabled = false; }
      });
      const actions = node("div", "nest-actions"); actions.append(button("btn", "取消", closeLayer), ok);
      cardEl.append(node("h3", "", "自定义应用"), node("p", "lead", "把你自己的程序或网址接进来，所有 Bot 都能用。"), tabs, nameW, progW, optW, urlW, err, actions);
      root.append(cardEl); layers.replaceChildren(root); layer = {opener};
      root.addEventListener("keydown", ev => { if (ev.key === "Escape") { ev.preventDefault(); ev.stopPropagation(); closeLayer(); } });
      setMode("program"); name.focus({preventScroll: true});
    }

    if (await load()) paint();
  }
  window.XBotApps = { mount, unmount: () => teardown() };
})();
