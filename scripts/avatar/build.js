'use strict';
// The browser renderer has no package dependencies or bundling step.
const fs = require('fs');
const path = require('path');
fs.copyFileSync(path.join(__dirname, 'entry.js'), path.join(__dirname, '../../internal/web/static/vendor/xbot-avatar.min.js'));
