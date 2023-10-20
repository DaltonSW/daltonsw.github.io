document.addEventListener('DOMContentLoaded', function() {
    // Array of potential background images
    const numImages = 17;

    // Randomly select a background image
    const randomBgImage = Math.floor(Math.random() * numImages);

    // Update the background image of the body
    document.body.style.backgroundImage = `url('backgrounds/${randomBgImage}.jpg')`;
});
