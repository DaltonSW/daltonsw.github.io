document.addEventListener('DOMContentLoaded', function() {
    randomBackground();
    const content = document.querySelector('.content');
    const profilePic = document.querySelector('.circular-image');

    function resetContentState(content) {
        document.querySelector('.content').classList.remove('content-active');
        document.querySelectorAll('.about-content, .links-content').forEach(section => {
            section.classList.remove('content-show');
            section.classList.add('content-hide');
        });
        content.classList.remove('content-active');
    }

    const links = document.querySelectorAll('.mainLinks a');

    // Add event listener to each link
    links.forEach(link => {
        link.addEventListener('click', function(e) {
            e.preventDefault();
            resetContentState(content);

            // Get the content associated with the clicked link
            const targetContent = document.querySelector(`.${link.getAttribute('data-content')}`);

            // Update content state
            document.querySelector('.content').classList.add('content-active');
            targetContent.classList.add('content-show');
            targetContent.classList.remove('content-hide');
        });
    });

    // Reset to main content view on profile picture click
    profilePic.addEventListener('click', function() {
        resetContentState(content);
    });
});

function randomBackground() {
    const numImages = 17;
    const randomBgImage = Math.floor(Math.random() * numImages);
    document.body.style.backgroundImage = `url('backgrounds/${randomBgImage}.jpg')`;
}

